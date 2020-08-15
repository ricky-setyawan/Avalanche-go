// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNoImportInputs = errors.New("no import inputs")
)

// ImportTx is a transaction that imports an asset from another blockchain.
type ImportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to consume the funds from
	SourceChain ids.ID `serialize:"true" json:"sourceChain"`

	// The inputs to this transaction
	ImportedIns []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
}

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *ImportTx) InputUTXOs() []*avax.UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, in := range t.ImportedIns {
		in.Symbol = true
		utxos = append(utxos, &in.UTXOID)
	}
	return utxos
}

// ConsumedAssetIDs returns the IDs of the assets this transaction consumes
func (t *ImportTx) ConsumedAssetIDs() ids.Set {
	assets := t.BaseTx.AssetIDs()
	for _, in := range t.ImportedIns {
		assets.Add(in.AssetID())
	}
	return assets
}

// AssetIDs returns the IDs of the assets this transaction depends on
func (t *ImportTx) AssetIDs() ids.Set {
	assets := t.BaseTx.AssetIDs()
	for _, in := range t.ImportedIns {
		assets.Add(in.AssetID())
	}
	return assets
}

// NumCredentials returns the number of expected credentials
func (t *ImportTx) NumCredentials() int { return t.BaseTx.NumCredentials() + len(t.ImportedIns) }

// SyntacticVerify that this transaction is well-formed.
func (t *ImportTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Codec,
	txFeeAssetID ids.ID,
	txFee uint64,
	numFxs int,
) error {
	switch {
	case t == nil:
		return errNilTx
	case t.SourceChain.IsZero():
		return errWrongBlockchainID
	case len(t.ImportedIns) == 0:
		return errNoImportInputs
	}

	if err := t.MetadataVerify(ctx); err != nil {
		return err
	}

	return avax.VerifyTx(
		txFee,
		txFeeAssetID,
		[][]*avax.TransferableInput{
			t.Ins,
			t.ImportedIns,
		},
		[][]*avax.TransferableOutput{t.Outs},
		c,
	)
}

// SemanticVerify that this transaction is well-formed.
func (t *ImportTx) SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	subnetID, err := vm.ctx.SNLookup.SubnetID(t.SourceChain)
	if err != nil {
		return err
	}
	if !vm.ctx.SubnetID.Equals(subnetID) || t.SourceChain.Equals(vm.ctx.ChainID) {
		return errWrongBlockchainID
	}

	if err := t.BaseTx.SemanticVerify(vm, uTx, creds); err != nil {
		return err
	}

	smDB := vm.ctx.SharedMemory.GetDatabase(t.SourceChain)
	defer vm.ctx.SharedMemory.ReleaseDatabase(t.SourceChain)

	state := avax.NewPrefixedState(smDB, vm.codec, vm.ctx.ChainID, t.SourceChain)

	offset := t.BaseTx.NumCredentials()
	for i, in := range t.ImportedIns {
		cred := creds[i+offset]

		fxIndex, err := vm.getFx(cred)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		utxoID := in.UTXOID.InputID()
		utxo, err := state.UTXO(utxoID)
		if err != nil {
			return err
		}
		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
			return errAssetIDMismatch
		}
		if !utxoAssetID.Equals(vm.avax) {
			return errWrongAssetID
		}

		if !vm.verifyFxUsage(fxIndex, inAssetID) {
			return errIncompatibleFx
		}

		if err := fx.VerifyTransfer(uTx, in.In, cred, utxo.Out); err != nil {
			return err
		}
	}
	for _, out := range t.Outs {
		fxIndex, err := vm.getFx(out.Out)
		if err != nil {
			return err
		}
		if assetID := out.AssetID(); !vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}
	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *ImportTx) ExecuteWithSideEffects(vm *VM, batch database.Batch) error {
	smDB := vm.ctx.SharedMemory.GetDatabase(t.SourceChain)
	defer vm.ctx.SharedMemory.ReleaseDatabase(t.SourceChain)

	vsmDB := versiondb.New(smDB)

	state := avax.NewPrefixedState(vsmDB, vm.codec, vm.ctx.ChainID, t.SourceChain)
	for _, in := range t.ImportedIns {
		utxoID := in.UTXOID.InputID()
		if err := state.SpendUTXO(utxoID); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}
