// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"math"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/core"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/btcsuite/btcutil/bech32"

	safemath "github.com/ava-labs/gecko/utils/math"
)

const (
	// For putting/getting values from state
	validatorsTypeID uint64 = iota
	chainsTypeID
	blockTypeID
	subnetsTypeID
	utxoTypeID
	utxoSetTypeID
	txTypeID
	statusTypeID

	platformAlias = "P"
	addressSep    = "-"

	// Delta is the synchrony bound used for safe decision making
	Delta = 10 * time.Second

	// BatchSize is the number of decision transaction to place into a block
	BatchSize = 30

	// NumberOfShares is the number of shares that a delegator is
	// rewarded
	NumberOfShares = 1000000

	// TODO: Turn these constants into governable parameters

	// InflationRate is the inflation rate from staking
	// TODO: make inflation rate non-zero
	InflationRate = 1.00

	// MinimumStakeAmount is the minimum amount of tokens one must bond to be a staker
	MinimumStakeAmount = 10 * units.MicroAva

	// MinimumStakingDuration is the shortest amount of time a staker can bond
	// their funds for.
	MinimumStakingDuration = 24 * time.Hour

	// MaximumStakingDuration is the longest amount of time a staker can bond
	// their funds for.
	MaximumStakingDuration = 365 * 24 * time.Hour

	droppedTxCacheSize = 50
)

var (
	// taken from https://stackoverflow.com/questions/25065055/what-is-the-maximum-time-time-in-go/32620397#32620397
	maxTime = time.Unix(1<<63-62135596801, 0) // 0 is used because we drop the nano-seconds

	timestampKey         = ids.NewID([32]byte{'t', 'i', 'm', 'e'})
	currentValidatorsKey = ids.NewID([32]byte{'c', 'u', 'r', 'r', 'e', 'n', 't'})
	pendingValidatorsKey = ids.NewID([32]byte{'p', 'e', 'n', 'd', 'i', 'n', 'g'})
	chainsKey            = ids.NewID([32]byte{'c', 'h', 'a', 'i', 'n', 's'})
	subnetsKey           = ids.NewID([32]byte{'s', 'u', 'b', 'n', 'e', 't', 's'})
)

var (
	errEndOfTime                = errors.New("program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred")
	errTimeTooAdvanced          = errors.New("this is proposing a time too far in the future")
	errNoPendingBlocks          = errors.New("no pending blocks")
	errUnsupportedFXs           = errors.New("unsupported feature extensions")
	errRegisteringType          = errors.New("error registering type with database")
	errMissingBlock             = errors.New("missing block")
	errInvalidLastAcceptedBlock = errors.New("last accepted block must be a decision block")
	errInvalidAddress           = errors.New("invalid address")
	errInvalidAddressSeperator  = errors.New("invalid address seperator")
	errInvalidAddressPrefix     = errors.New("invalid address prefix")
	errInvalidAddressSuffix     = errors.New("invalid address suffix")
	errEmptyAddressPrefix       = errors.New("empty address prefix")
	errEmptyAddressSuffix       = errors.New("empty address suffix")
	errInvalidID                = errors.New("invalid ID")
	errDSCantValidate           = errors.New("new blockchain can't be validated by default Subnet")
)

// Codec does serialization and deserialization
var Codec codec.Codec

func init() {
	Codec = codec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		Codec.RegisterType(&ProposalBlock{}),
		Codec.RegisterType(&Abort{}),
		Codec.RegisterType(&Commit{}),
		Codec.RegisterType(&StandardBlock{}),
		Codec.RegisterType(&AtomicBlock{}),

		// The Fx is registered here because this is the same place it is
		// registered in the AVM. This ensures that the typeIDs match up for
		// utxos in shared memory.
		Codec.RegisterType(&secp256k1fx.TransferInput{}),
		Codec.RegisterType(&secp256k1fx.MintOutput{}),
		Codec.RegisterType(&secp256k1fx.TransferOutput{}),
		Codec.RegisterType(&secp256k1fx.MintOperation{}),
		Codec.RegisterType(&secp256k1fx.Credential{}),
		Codec.RegisterType(&secp256k1fx.Input{}),
		Codec.RegisterType(&secp256k1fx.OutputOwners{}),

		Codec.RegisterType(&UnsignedAddDefaultSubnetValidatorTx{}),
		Codec.RegisterType(&UnsignedAddNonDefaultSubnetValidatorTx{}),
		Codec.RegisterType(&UnsignedAddDefaultSubnetDelegatorTx{}),

		Codec.RegisterType(&UnsignedCreateChainTx{}),
		Codec.RegisterType(&UnsignedCreateSubnetTx{}),

		Codec.RegisterType(&UnsignedImportTx{}),
		Codec.RegisterType(&UnsignedExportTx{}),

		Codec.RegisterType(&UnsignedAdvanceTimeTx{}),
		Codec.RegisterType(&UnsignedRewardValidatorTx{}),

		Codec.RegisterType(&StakeableLockIn{}),
		Codec.RegisterType(&StakeableLockOut{}),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM implements the snowman.ChainVM interface
type VM struct {
	*core.SnowmanVM

	// Node's validator manager
	// Maps Subnets --> nodes in the Subnet
	validators validators.Manager

	// true if the node is being run with staking enabled
	stakingEnabled bool

	// The node's chain manager
	chainManager chains.Manager

	// AVAX asset ID
	avaxAssetID ids.ID

	// AVM is the ID of the ava virtual machine
	avm ids.ID

	fx    Fx
	codec codec.Codec

	// Used to create and use keys.
	factory crypto.FactorySECP256K1R

	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	// Key: block ID
	// Value: the block
	currentBlocks map[[32]byte]Block

	// Transactions that have not been put into blocks yet
	unissuedEvents      *EventHeap
	unissuedDecisionTxs []*DecisionTx
	unissuedAtomicTxs   []*AtomicTx

	// Tx fee burned by a transaction
	txFee uint64

	// This timer goes off when it is time for the next validator to add/leave the validator set
	// When it goes off resetTimer() is called, triggering creation of a new block
	timer *timer.Timer

	// Contains the IDs of transactions recently dropped because they failed verification.
	// These txs may be re-issued and put into accepted blocks, so check the database
	// to see if it was later committed/aborted before reporting that it's dropped
	droppedTxCache cache.LRU
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.Validators] must be set before this function is called.
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	msgs chan<- common.Message,
	_ []*common.Fx,
) error {
	ctx.Log.Verbo("initializing platform chain")
	// Initialize the inner VM, which has a lot of boiler-plate logic
	vm.SnowmanVM = &core.SnowmanVM{}
	if err := vm.SnowmanVM.Initialize(ctx, db, vm.unmarshalBlockFunc, msgs); err != nil {
		return err
	}
	vm.fx = &secp256k1fx.Fx{}

	vm.codec = codec.NewDefault()
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}
	vm.codec = Codec

	vm.droppedTxCache = cache.LRU{Size: droppedTxCacheSize}

	// Register this VM's types with the database so we can get/put structs to/from it
	vm.registerDBTypes()

	// If the database is empty, create the platform chain anew using
	// the provided genesis state
	if !vm.DBInitialized() {
		genesis := &Genesis{}
		if err := Codec.Unmarshal(genesisBytes, genesis); err != nil {
			return err
		}
		if err := genesis.Initialize(); err != nil {
			return err
		}

		// Persist UTXOs that exist at genesis
		for _, utxo := range genesis.UTXOs {
			if err := vm.putUTXO(vm.DB, utxo); err != nil {
				return err
			}
		}

		// Persist default subnet validator set at genesis
		if err := vm.putCurrentValidators(vm.DB, genesis.Validators, constants.DefaultSubnetID); err != nil {
			return err
		}

		// Persist the subnets that exist at genesis (none do)
		if err := vm.putSubnets(vm.DB, []*DecisionTx{}); err != nil {
			return fmt.Errorf("error putting genesis subnets: %v", err)
		}

		// Ensure all chains that the genesis bytes say to create
		// have the right network ID
		filteredChains := []*DecisionTx{}
		for _, chain := range genesis.Chains {
			unsignedChain, ok := chain.UnsignedDecisionTx.(*UnsignedCreateChainTx)
			if !ok {
				vm.Ctx.Log.Warn("invalid tx type in genesis chains")
				continue
			}
			if unsignedChain.NetworkID != vm.Ctx.NetworkID {
				vm.Ctx.Log.Warn("chain has networkID %d, expected %d", unsignedChain.NetworkID, vm.Ctx.NetworkID)
				continue
			}
			filteredChains = append(filteredChains, chain)
		}

		// Persist the chains that exist at genesis
		if err := vm.putChains(vm.DB, filteredChains); err != nil {
			return err
		}

		// Persist the platform chain's timestamp at genesis
		time := time.Unix(int64(genesis.Timestamp), 0)
		if err := vm.State.PutTime(vm.DB, timestampKey, time); err != nil {
			return err
		}

		// There are no pending stakers at genesis
		if err := vm.putPendingValidators(vm.DB, &EventHeap{SortByStartTime: true}, constants.DefaultSubnetID); err != nil {
			return err
		}

		// Create the genesis block and save it as being accepted (We don't just
		// do genesisBlock.Accept() because then it'd look for genesisBlock's
		// non-existent parent)
		genesisBlock, err := vm.newCommitBlock(ids.Empty, 0)
		if err != nil {
			return err
		}
		if err := vm.State.PutBlock(vm.DB, genesisBlock); err != nil {
			return err
		}
		genesisBlock.onAcceptDB = versiondb.New(vm.DB)
		if err := genesisBlock.CommonBlock.Accept(); err != nil {
			return err
		}

		vm.SetDBInitialized()

		if err := vm.DB.Commit(); err != nil {
			return err
		}
	}

	// Transactions from clients that have not yet been put into blocks
	// and added to consensus
	vm.unissuedEvents = &EventHeap{SortByStartTime: true}

	vm.currentBlocks = make(map[[32]byte]Block)
	vm.timer = timer.NewTimer(func() {
		vm.Ctx.Lock.Lock()
		defer vm.Ctx.Lock.Unlock()

		vm.resetTimer()
	})
	go ctx.Log.RecoverAndPanic(vm.timer.Dispatch)

	if err := vm.initSubnets(); err != nil {
		ctx.Log.Error("failed to initialize Subnets: %s", err)
		return err
	}

	// Create all of the chains that the database says exist
	if err := vm.initBlockchains(); err != nil {
		vm.Ctx.Log.Warn("could not retrieve existing chains from database: %s", err)
		return err
	}

	lastAcceptedID := vm.LastAccepted()
	vm.Ctx.Log.Info("Initializing last accepted block as %s", lastAcceptedID)

	// Build off the most recently accepted block
	vm.SetPreference(lastAcceptedID)

	// Sanity check to make sure the DB is in a valid state
	lastAcceptedIntf, err := vm.getBlock(lastAcceptedID)
	if err != nil {
		vm.Ctx.Log.Error("Error fetching the last accepted block (%s), %s", vm.Preferred(), err)
		return err
	}
	if _, ok := lastAcceptedIntf.(decision); !ok {
		vm.Ctx.Log.Fatal("The last accepted block, %s, must always be a decision block", lastAcceptedID)
		return errInvalidLastAcceptedBlock
	}

	return nil
}

// Queue [tx] to be put into a block
func (vm *VM) issueTx(tx interface{}) error {
	switch tx := tx.(type) {
	case *ProposalTx:
		txBytes, err := vm.codec.Marshal(tx)
		if err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		if err := tx.initialize(vm, txBytes); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		vm.unissuedEvents.Add(tx)
	case *DecisionTx:
		txBytes, err := vm.codec.Marshal(tx)
		if err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		if err := tx.initialize(vm, txBytes); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, tx)
	case *AtomicTx:
		txBytes, err := vm.codec.Marshal(tx)
		if err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		if err := tx.initialize(vm, txBytes); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		vm.unissuedAtomicTxs = append(vm.unissuedAtomicTxs, tx)
	default:
		return errors.New("Could not parse given tx. Provided tx needs to be a ProposalTx, DecisionTx, or AtomicTx")
	}
	vm.resetTimer()
	return nil
}

// Create all chains that exist that this node validates
// Can only be called after initSubnets()
func (vm *VM) initBlockchains() error {
	vm.Ctx.Log.Info("initializing blockchains")
	blockchains, err := vm.getChains(vm.DB) // get blockchains that exist
	if err != nil {
		return err
	}

	for _, chain := range blockchains {
		vm.createChain(chain)
	}
	return nil
}

// Set the node's validator manager to be up to date
func (vm *VM) initSubnets() error {
	vm.Ctx.Log.Info("initializing Subnets")
	subnets, err := vm.getSubnets(vm.DB)
	if err != nil {
		return err
	}

	if err := vm.updateValidators(constants.DefaultSubnetID); err != nil {
		return err
	}

	for _, subnet := range subnets {
		if err := vm.updateValidators(subnet.ID()); err != nil {
			return err
		}
	}

	return nil
}

// Create the blockchain described in [tx], but only if this node is a member of
// the Subnet that validates the chain
func (vm *VM) createChain(tx *DecisionTx) {
	unsignedTx, ok := tx.UnsignedDecisionTx.(*UnsignedCreateChainTx)
	if !ok {
		// Invalid tx type
		return
	}
	// The validators that compose the Subnet that validates this chain
	validators, subnetExists := vm.validators.GetValidatorSet(unsignedTx.SubnetID)
	if !subnetExists {
		vm.Ctx.Log.Error("blockchain %s validated by Subnet %s but couldn't get that Subnet. Blockchain not created")
		return
	}
	if vm.stakingEnabled && // Staking is enabled, so nodes might not validate all chains
		!constants.DefaultSubnetID.Equals(unsignedTx.SubnetID) && // All nodes must validate the default subnet
		!validators.Contains(vm.Ctx.NodeID) { // This node doesn't validate this blockchain
		return
	}

	chainParams := chains.ChainParameters{
		ID:          tx.ID(),
		SubnetID:    unsignedTx.SubnetID,
		GenesisData: unsignedTx.GenesisData,
		VMAlias:     unsignedTx.VMID.String(),
	}
	for _, fxID := range unsignedTx.FxIDs {
		chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
	}
	vm.chainManager.CreateChain(chainParams)
}

// Bootstrapping marks this VM as bootstrapping
func (vm *VM) Bootstrapping() error { return vm.fx.Bootstrapping() }

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error { return vm.fx.Bootstrapped() }

// Shutdown this blockchain
func (vm *VM) Shutdown() error {
	if vm.timer == nil {
		return nil
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	vm.Ctx.Lock.Unlock()
	vm.timer.Stop()
	vm.Ctx.Lock.Lock()

	return vm.DB.Close()
}

// BuildBlock builds a block to be added to consensus
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.Ctx.Log.Debug("in BuildBlock")
	// TODO: Add PreferredHeight() to core.snowmanVM
	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block's height: %w", err)
	}

	preferredID := vm.Preferred()

	// If there are pending decision txs, build a block with a batch of them
	if len(vm.unissuedDecisionTxs) > 0 {
		numTxs := BatchSize
		if numTxs > len(vm.unissuedDecisionTxs) {
			numTxs = len(vm.unissuedDecisionTxs)
		}
		var txs []*DecisionTx
		txs, vm.unissuedDecisionTxs = vm.unissuedDecisionTxs[:numTxs], vm.unissuedDecisionTxs[numTxs:]
		blk, err := vm.newStandardBlock(preferredID, preferredHeight+1, txs)
		if err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := blk.Verify(); err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			vm.resetTimer()
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// If there is a pending atomic tx, build a block with it
	if len(vm.unissuedAtomicTxs) > 0 {
		tx := vm.unissuedAtomicTxs[0]
		vm.unissuedAtomicTxs = vm.unissuedAtomicTxs[1:]
		blk, err := vm.newAtomicBlock(preferredID, preferredHeight+1, *tx)
		if err != nil {
			return nil, err
		}
		if err := blk.Verify(); err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// Get the preferred block (which we want to build off)
	preferred, err := vm.getBlock(preferredID)
	vm.Ctx.Log.AssertNoError(err)

	// The database if the preferred block were to be accepted
	var db database.Database
	// The preferred block should always be a decision block
	if preferred, ok := preferred.(decision); ok {
		db = preferred.onAccept()
	} else {
		return nil, errInvalidBlockType
	}

	// The chain time if the preferred block were to be committed
	currentChainTimestamp, err := vm.getTimestamp(db)
	if err != nil {
		return nil, err
	}
	if !currentChainTimestamp.Before(maxTime) {
		return nil, errEndOfTime
	}

	// If the chain time would be the time for the next default subnet validator to leave,
	// then we create a block that removes the validator and proposes they receive a validator reward
	currentValidators, err := vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get validator set: %w", err)
	}
	nextValidatorEndtime := maxTime
	if currentValidators.Len() > 0 {
		nextValidatorEndtime = currentValidators.Peek().UnsignedProposalTx.(TimedTx).EndTime()
	}
	if currentChainTimestamp.Equal(nextValidatorEndtime) {
		stakerTx := currentValidators.Peek()
		rewardValidatorTx, err := vm.newRewardValidatorTx(stakerTx.ID())
		if err != nil {
			return nil, err
		}
		blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *rewardValidatorTx)
		if err != nil {
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// If local time is >= time of the next validator set change,
	// propose moving the chain time forward
	nextValidatorStartTime := vm.nextValidatorChangeTime(db /*start=*/, true)
	nextValidatorEndTime := vm.nextValidatorChangeTime(db /*start=*/, false)

	nextValidatorSetChangeTime := nextValidatorStartTime
	if nextValidatorEndTime.Before(nextValidatorStartTime) {
		nextValidatorSetChangeTime = nextValidatorEndTime
	}

	localTime := vm.clock.Time()
	if !localTime.Before(nextValidatorSetChangeTime) { // time is at or after the time for the next validator to join/leave
		advanceTimeTx, err := vm.newAdvanceTimeTx(nextValidatorSetChangeTime)
		if err != nil {
			return nil, err
		}
		blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx)
		if err != nil {
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// Propose adding a new validator but only if their start time is in the
	// future relative to local time (plus Delta)
	syncTime := localTime.Add(Delta)
	for vm.unissuedEvents.Len() > 0 {
		tx := vm.unissuedEvents.Remove()
		utx := tx.UnsignedProposalTx.(TimedTx)
		if !syncTime.After(utx.StartTime()) {
			blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *tx)
			if err != nil {
				return nil, err
			}
			if err := vm.State.PutBlock(vm.DB, blk); err != nil {
				return nil, err
			}
			return blk, vm.DB.Commit()
		}
		vm.Ctx.Log.Debug("dropping tx to add validator because start time too late")
	}

	vm.Ctx.Log.Debug("BuildBlock returning error (no blocks)")
	return nil, errNoPendingBlocks
}

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(bytes []byte) (snowman.Block, error) {
	blockInterface, err := vm.unmarshalBlockFunc(bytes)
	if err != nil {
		return nil, errors.New("problem parsing block")
	}
	block, ok := blockInterface.(snowman.Block)
	if !ok { // in practice should never happen because unmarshalBlockFunc returns a snowman.Block
		return nil, errors.New("problem parsing block")
	}
	// If we have seen this block before, return it with the most up-to-date info
	if block, err := vm.GetBlock(block.ID()); err == nil {
		return block, nil
	}
	vm.State.PutBlock(vm.DB, block)
	vm.DB.Commit()
	return block, nil
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(blkID ids.ID) (snowman.Block, error) { return vm.getBlock(blkID) }

func (vm *VM) getBlock(blkID ids.ID) (Block, error) {
	// If block is in memory, return it.
	if blk, exists := vm.currentBlocks[blkID.Key()]; exists {
		return blk, nil
	}
	// Block isn't in memory. If block is in database, return it.
	blkInterface, err := vm.State.GetBlock(vm.DB, blkID)
	if err != nil {
		return nil, err
	}
	if block, ok := blkInterface.(Block); ok {
		return block, nil
	}
	return nil, errors.New("block not found")
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(blkID ids.ID) {
	if !blkID.Equals(vm.Preferred()) {
		vm.SnowmanVM.SetPreference(blkID)
		vm.resetTimer()
	}
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	// Create a service with name "platform"
	handler := vm.SnowmanVM.NewHandler("platform", &Service{vm: vm})
	return map[string]*common.HTTPHandler{"": handler}
}

// CreateStaticHandlers implements the snowman.ChainVM interface
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	// Static service's name is platform
	handler := vm.SnowmanVM.NewHandler("platform", &StaticService{})
	return map[string]*common.HTTPHandler{"": handler}
}

// Check if there is a block ready to be added to consensus
// If so, notify the consensus engine
func (vm *VM) resetTimer() {
	// If there is a pending transaction, trigger building of a block with that
	// transaction
	if len(vm.unissuedDecisionTxs) > 0 || len(vm.unissuedAtomicTxs) > 0 {
		vm.SnowmanVM.NotifyBlockReady()
		return
	}

	// Get the preferred block
	preferredIntf, err := vm.getBlock(vm.Preferred())
	if err != nil {
		vm.Ctx.Log.Error("Error fetching the preferred block (%s), %s", vm.Preferred(), err)
		return
	}

	// The database if the preferred block were to be committed
	var db database.Database
	// The preferred block should always be a decision block
	if preferred, ok := preferredIntf.(decision); ok {
		db = preferred.onAccept()
	} else {
		vm.Ctx.Log.Error("The preferred block, %s, should always be a decision block", vm.Preferred())
		return
	}

	// The chain time if the preferred block were to be committed
	timestamp, err := vm.getTimestamp(db)
	if err != nil {
		vm.Ctx.Log.Error("could not retrieve timestamp from database")
		return
	}
	if timestamp.Equal(maxTime) {
		vm.Ctx.Log.Error("Program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred.")
		return
	}

	nextDSValidatorEndTime := vm.nextSubnetValidatorChangeTime(db, constants.DefaultSubnetID, false)
	if timestamp.Equal(nextDSValidatorEndTime) {
		vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeRewardValidator
		return
	}

	// If local time is >= time of the next change in the validator set,
	// propose moving forward the chain timestamp
	nextValidatorStartTime := vm.nextValidatorChangeTime(db, true)
	nextValidatorEndTime := vm.nextValidatorChangeTime(db, false)

	nextValidatorSetChangeTime := nextValidatorStartTime
	if nextValidatorEndTime.Before(nextValidatorStartTime) {
		nextValidatorSetChangeTime = nextValidatorEndTime
	}

	localTime := vm.clock.Time()
	if !localTime.Before(nextValidatorSetChangeTime) { // time is at or after the time for the next validator to join/leave
		vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeTimestamp
		return
	}

	syncTime := localTime.Add(Delta)
	for vm.unissuedEvents.Len() > 0 {
		if !syncTime.After(vm.unissuedEvents.Peek().UnsignedProposalTx.(TimedTx).StartTime()) {
			vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeAddValidator
			return
		}
		// If the tx doesn't meet the synchrony bound, drop it
		vm.unissuedEvents.Remove()
		vm.Ctx.Log.Debug("dropping tx to add validator because its start time has passed")
	}

	waitTime := nextValidatorSetChangeTime.Sub(localTime)
	vm.Ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextValidatorSetChangeTime, waitTime)

	// Wake up when it's time to add/remove the next validator
	vm.timer.SetTimeoutIn(waitTime)
}

// If [start], returns the time at which the next validator (of any subnet) in the pending set starts validating
// Otherwise, returns the time at which the next validator (of any subnet) stops validating
// If no such validator is found, returns maxTime
func (vm *VM) nextValidatorChangeTime(db database.Database, start bool) time.Time {
	earliest := vm.nextSubnetValidatorChangeTime(db, constants.DefaultSubnetID, start)
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return earliest
	}
	for _, subnet := range subnets {
		t := vm.nextSubnetValidatorChangeTime(db, subnet.ID(), start)
		if t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

func (vm *VM) nextSubnetValidatorChangeTime(db database.Database, subnetID ids.ID, start bool) time.Time {
	var validators *EventHeap
	var err error
	if start {
		validators, err = vm.getPendingValidators(db, subnetID)
	} else {
		validators, err = vm.getCurrentValidators(db, subnetID)
	}
	if err != nil {
		vm.Ctx.Log.Error("couldn't get validators of subnet with ID %s: %v", subnetID, err)
		return maxTime
	}
	if validators.Len() == 0 {
		vm.Ctx.Log.Verbo("subnet, %s, has no validators", subnetID)
		return maxTime
	}
	return validators.Timestamp()
}

// Returns:
// 1) The validator set of subnet with ID [subnetID] when timestamp is advanced to [timestamp]
// 2) The pending validator set of subnet with ID [subnetID] when timestamp is advanced to [timestamp]
// 3) The IDs of the validators that start validating [subnetID] between now and [timestamp]
// 4) The IDs of the validators that stop validating [subnetID] between now and [timestamp]
// Note that this method will not remove validators from the current validator set of the default subnet.
// That happens in reward blocks.
func (vm *VM) calculateValidators(db database.Database, timestamp time.Time, subnetID ids.ID) (current,
	pending *EventHeap, started, stopped ids.ShortSet, err error) {
	// remove validators whose end time <= [timestamp]
	current, err = vm.getCurrentValidators(db, subnetID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !subnetID.Equals(constants.DefaultSubnetID) { // validators of default subnet removed in rewardValidatorTxs, not here
		for current.Len() > 0 {
			next := current.Peek().UnsignedProposalTx.(*UnsignedAddNonDefaultSubnetValidatorTx) // current validator with earliest end time
			if timestamp.Before(next.EndTime()) {
				break
			}
			current.Remove()
			stopped.Add(next.Vdr().ID())
		}
	}
	pending, err = vm.getPendingValidators(db, subnetID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for pending.Len() > 0 {
		nextTx := pending.Peek() // pending staker with earliest start time
		switch tx := nextTx.UnsignedProposalTx.(type) {
		case *UnsignedAddDefaultSubnetValidatorTx:
			if timestamp.Before(tx.StartTime()) {
				break
			}
			current.Add(nextTx)
			pending.Remove()
			started.Add(tx.Vdr().ID())
		case *UnsignedAddNonDefaultSubnetValidatorTx:
			if timestamp.Before(tx.StartTime()) {
				break
			}
			current.Add(nextTx)
			pending.Remove()
			started.Add(tx.Vdr().ID())
		}
	}
	return current, pending, started, stopped, nil
}

func (vm *VM) getValidators(validatorEvents *EventHeap) []validators.Validator {
	vdrMap := make(map[[20]byte]*Validator, validatorEvents.Len())
	for _, event := range validatorEvents.Txs {
		var vdr validators.Validator
		switch tx := event.UnsignedProposalTx.(type) {
		case *UnsignedAddDefaultSubnetValidatorTx:
			vdr = tx.Vdr()
		case *UnsignedAddDefaultSubnetDelegatorTx:
			vdr = tx.Vdr()
		case *UnsignedAddNonDefaultSubnetValidatorTx:
			vdr = tx.Vdr()
		default:
			continue
		}
		vdrID := vdr.ID()
		vdrKey := vdrID.Key()
		validator, exists := vdrMap[vdrKey]
		if !exists {
			validator = &Validator{NodeID: vdrID}
			vdrMap[vdrKey] = validator
		}
		weight, err := safemath.Add64(validator.Wght, vdr.Weight())
		if err != nil {
			weight = math.MaxUint64
		}
		validator.Wght = weight
	}

	vdrList := make([]validators.Validator, len(vdrMap))
	i := 0
	for _, validator := range vdrMap {
		vdrList[i] = validator
		i++
	}
	return vdrList
}

// update the node's validator manager to contain the current validator set of the given Subnet
func (vm *VM) updateValidators(subnetID ids.ID) error {
	validatorSet, subnetInitialized := vm.validators.GetValidatorSet(subnetID)
	if !subnetInitialized { // validator manager doesn't know about this subnet yet
		validatorSet = validators.NewSet()
		vm.validators.PutValidatorSet(subnetID, validatorSet)
	}

	currentValidators, err := vm.getCurrentValidators(vm.DB, subnetID)
	if err != nil {
		return err
	}

	validators := vm.getValidators(currentValidators)
	validatorSet.Set(validators)
	return nil
}

// Codec ...
func (vm *VM) Codec() codec.Codec { return vm.codec }

// Clock ...
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Logger ...
func (vm *VM) Logger() logging.Logger { return vm.Ctx.Log }

// GetAtomicUTXOs returns the utxos that at least one of the provided addresses is
// referenced in.
func (vm *VM) GetAtomicUTXOs(addrs ids.Set) ([]*ava.UTXO, error) {
	smDB := vm.Ctx.SharedMemory.GetDatabase(vm.avm)
	defer vm.Ctx.SharedMemory.ReleaseDatabase(vm.avm)

	state := ava.NewPrefixedState(smDB, vm.codec)

	utxoIDs := ids.Set{}
	for _, addr := range addrs.List() {
		utxos, err := state.AVMFunds(addr)
		if err != nil {
			return nil, err
		}
		utxoIDs.Add(utxos...)
	}

	utxos := []*ava.UTXO{}
	for _, utxoID := range utxoIDs.List() {
		utxo, err := state.AVMUTXO(utxoID)
		if err != nil {
			return nil, err
		}
		utxos = append(utxos, utxo)
	}
	return utxos, nil
}

func splitAddress(addrStr string) (string, string, error) {
	if count := strings.Count(addrStr, addressSep); count != 1 {
		return "", "", errInvalidAddressSeperator
	}
	addrParts := strings.SplitN(addrStr, addressSep, 2)
	prefix := addrParts[0]
	if prefix == "" {
		return "", "", errEmptyAddressPrefix
	}
	suffix := addrParts[1]
	if suffix == "" {
		return "", "", errEmptyAddressSuffix
	}
	return prefix, suffix, nil
}

// ParseAddress returns a decoded Platform Chain address.
// addrStr is an encoded address, of the form "P-<bech32 encoded bytes>".
func (vm *VM) ParseAddress(addrStr string) (ids.ShortID, error) {
	networkID := vm.Ctx.NetworkID
	var hrp string = constants.FallbackHRP
	if _, ok := constants.NetworkIDToHRP[networkID]; ok {
		hrp = constants.NetworkIDToHRP[networkID]
	}
	if addrStr == "" {
		return ids.ShortID{}, errEmptyAddress
	}
	prefix, suffix, err := splitAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if prefix != platformAlias {
		return ids.ShortID{}, errInvalidAddressPrefix
	}

	rawHRP, decoded, err := bech32.Decode(suffix)
	if err != nil {
		return ids.ShortID{}, err
	}
	if rawHRP != hrp {
		return ids.ShortID{}, fmt.Errorf("%w: %v", errInvalidAddress, err)
	}
	return ids.ToShortID(decoded)
}

// FormatAddress returns an encoded Platform Chain address, of the form
// "P-<bech32 encoded bytes>".
func (vm *VM) FormatAddress(addrID ids.ShortID) (string, error) {
	networkID := vm.Ctx.NetworkID
	var hrp string = constants.FallbackHRP
	if _, ok := constants.NetworkIDToHRP[networkID]; ok {
		hrp = constants.NetworkIDToHRP[networkID]
	}
	addr, err := bech32.Encode(hrp, addrID.Bytes())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", platformAlias, addressSep, addr), nil
}
