// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Block = &preForkBlock{}

// preForkBlock implements proposervm.Block
type preForkBlock struct {
	snowman.Block
	vm *VM
}

func (b *preForkBlock) Parent() snowman.Block {
	return &preForkBlock{
		Block: b.Block.Parent(),
		vm:    b.vm,
	}
}

func (b *preForkBlock) Verify() error {
	parent, err := b.vm.getBlock(b.Block.Parent().ID())
	if err != nil {
		return err
	}
	return parent.verifyPreForkChild(b)
}

func (b *preForkBlock) Options() ([2]snowman.Block, error) {
	oracleBlk, ok := b.Block.(snowman.OracleBlock)
	if !ok {
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	opts, err := oracleBlk.Options()
	if err != nil {
		return opts, nil
	}
	res := [2]snowman.Block{
		&preForkBlock{
			Block: opts[0],
			vm:    b.vm,
		},
		&preForkBlock{
			Block: opts[1],
			vm:    b.vm,
		},
	}
	return res, nil
}

func (b *preForkBlock) verifyPreForkChild(child *preForkBlock) error {
	parentTimestamp := b.Timestamp()
	if !parentTimestamp.Before(b.vm.activationTime) {
		return errProposersActivated
	}

	return child.Block.Verify()
}

func (b *preForkBlock) verifyPostForkChild(child *postForkBlock) error {
	// TODO: verify that [childPChainHeight] is <= the P-chain's current height
	childPChainHeight := child.PChainHeight()
	_ = childPChainHeight

	expectedInnerParentID := b.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	// This block is expected to be the fork block
	parentTimestamp := b.Timestamp()
	if parentTimestamp.Before(b.vm.activationTime) {
		return errProposersNotActivated
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := b.vm.Time().Add(syncBound)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	// Verify the signature of the node
	if err := child.Block.Verify(); err != nil {
		return err
	}

	// validate core block, only once
	if !b.vm.Tree.Contains(child.innerBlk) {
		if err := child.innerBlk.Verify(); err != nil {
			return err
		}
		b.vm.Tree.Add(child.innerBlk)
	}

	b.vm.verifiedBlocks[child.ID()] = child
	return nil
}

func (b *preForkBlock) verifyPostForkOption(child *postForkOption) error {
	return errProposersActivated // TODO: find better error
}

func (b *preForkBlock) buildChild(innerBlock snowman.Block) (Block, error) {
	parentTimestamp := b.Timestamp()
	if parentTimestamp.Before(b.vm.activationTime) {
		// The chain hasn't forked yet
		res := &preForkBlock{
			Block: innerBlock,
			vm:    b.vm,
		}
		b.vm.ctx.Log.Debug("Snowman++ build pre-fork block %s - timestamp parent block %v",
			res.ID(), b.Timestamp().Unix())

		return res, nil
	}

	// The chain is currently forking

	parentID := b.ID()
	newTimestamp := b.vm.Time()
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	pChainHeight, err := b.vm.ctx.ValidatorVM.GetCurrentHeight()
	if err != nil {
		return nil, err
	}

	statelessBlock, err := block.Build(
		parentID,
		newTimestamp,
		pChainHeight,
		b.vm.ctx.StakingCert.Leaf,
		innerBlock.Bytes(),
		b.vm.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		return nil, err
	}

	blk := &postForkBlock{
		Block:    statelessBlock,
		vm:       b.vm,
		innerBlk: innerBlock,
		status:   choices.Processing,
	}

	b.vm.ctx.Log.Debug("Snowman++ build post-fork block %s - timestamp %v, timestamp parent block %v",
		blk.ID(), blk.Timestamp().Unix(), b.Timestamp().Unix())
	return blk, b.vm.storePostForkBlock(blk)
}
