// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errAssetIDMismatch          = errors.New("asset IDs in the input don't match the utxo")
	errWrongNumberOfCredentials = errors.New("should have the same number of credentials as inputs")

	_ VerifiableUnsignedAtomicTx = VerifiableUnsignedImportTx{}
)

type VerifiableUnsignedImportTx struct {
	*transactions.UnsignedImportTx `serialize:"true"`
}

// InputUTXOs returns the UTXOIDs of the imported funds
func (tx VerifiableUnsignedImportTx) InputUTXOs() ids.Set {
	set := ids.NewSet(len(tx.ImportedInputs))
	for _, in := range tx.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

// SemanticVerify this transaction is valid.
func (tx VerifiableUnsignedImportTx) SemanticVerify(
	vm *VM,
	parentState MutableState,
	stx *transactions.SignedTx,
) (VersionedState, TxError) {
	syntacticCtx := transactions.AtomicTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          Codec,
		AvmID:      vm.ctx.XChainID,
		FeeAssetID: vm.ctx.AVAXAssetID,
		FeeAmount:  vm.TxFee,
	}
	if err := tx.SyntacticVerify(syntacticCtx); err != nil {
		return nil, permError{err}
	}

	utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
	for index, input := range tx.Ins {
		utxo, err := parentState.GetUTXO(input.InputID())
		if err != nil {
			return nil, tempError{
				fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err),
			}
		}
		utxos[index] = utxo
	}

	if vm.bootstrapped {
		utxoIDs := make([][]byte, len(tx.ImportedInputs))
		for i, in := range tx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return nil, tempError{
				fmt.Errorf("failed to get shared memory: %w", err),
			}
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, tempError{
					fmt.Errorf("failed to unmarshal UTXO: %w", err),
				}
			}
			utxos[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		if err := vm.semanticVerifySpendUTXOs(tx, utxos, ins, tx.Outs, stx.Creds, vm.TxFee, vm.ctx.AVAXAssetID); err != nil {
			return nil, err
		}
	}

	// Set up the state if this tx is committed
	newState := newVersionedState(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	// Consume the UTXOS
	consumeInputs(newState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(newState, txID, vm.ctx.AVAXAssetID, tx.Outs)
	return newState, nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx VerifiableUnsignedImportTx) Accept(ctx *snow.Context, batch database.Batch) error {
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		utxoIDs[i] = utxoID[:]
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{tx.SourceChain: {RemoveRequests: utxoIDs}}, batch)
}

// Create a new transaction
func (vm *VM) newImportTx(
	chainID ids.ID, // chain to import from
	to ids.ShortID, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*transactions.SignedTx, error) {
	if vm.ctx.XChainID != chainID {
		return nil, transactions.ErrWrongChainID
	}

	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(chainID, kc.Addresses(), ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := uint64(0)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		if utxo.AssetID() != vm.ctx.AVAXAssetID {
			continue
		}
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		importedAmount, err = math.Add64(importedAmount, input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)

	if importedAmount == 0 {
		return nil, errNoFunds // No imported UTXOs were spendable
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	if importedAmount < vm.TxFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, _, baseSigners, err = vm.stake(keys, 0, vm.TxFee-importedAmount, changeAddr)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > vm.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - vm.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	// Create the transaction
	utx := VerifiableUnsignedImportTx{
		UnsignedImportTx: &transactions.UnsignedImportTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    vm.ctx.NetworkID,
				BlockchainID: vm.ctx.ChainID,
				Outs:         outs,
				Ins:          ins,
			}},
			SourceChain:    chainID,
			ImportedInputs: importedInputs,
		},
	}
	tx := &transactions.SignedTx{UnsignedTx: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}

	syntacticCtx := transactions.AtomicTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          Codec,
		AvmID:      vm.ctx.XChainID,
		FeeAssetID: vm.ctx.AVAXAssetID,
		FeeAmount:  vm.TxFee,
	}
	return tx, utx.SyntacticVerify(syntacticCtx)
}
