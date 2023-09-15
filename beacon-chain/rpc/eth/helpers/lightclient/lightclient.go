package lightclient

import (
	"bytes"
	"context"
	"fmt"

	"github.com/prysmaticlabs/prysm/v4/beacon-chain/rpc/core"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/v4/config/params"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/interfaces"
	types "github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
	ethpbv1 "github.com/prysmaticlabs/prysm/v4/proto/eth/v1"
	ethpbv2 "github.com/prysmaticlabs/prysm/v4/proto/eth/v2"
	"github.com/prysmaticlabs/prysm/v4/proto/migration"
)

func NewLightClientOptimisticUpdateFromBeaconState(
	ctx context.Context,
	config *params.BeaconChainConfig,
	state state.BeaconState,
	block interfaces.ReadOnlySignedBeaconBlock,
	attestedState state.BeaconState) (*ethpbv2.LightClientUpdate, *core.RpcError) {
	// assert compute_epoch_at_slot(attested_state.slot) >= ALTAIR_FORK_EPOCH
	attestedEpoch := types.Epoch(uint64(attestedState.Slot()) / uint64(config.SlotsPerEpoch))
	if attestedEpoch < types.Epoch(config.AltairForkEpoch) {
		return nil, &core.RpcError{Err: fmt.Errorf("Invalid attested epoch: %d", attestedEpoch), Reason: core.BadRequest}
	}

	// assert sum(block.message.body.sync_aggregate.sync_committee_bits) >= MIN_SYNC_COMMITTEE_PARTICIPANTS
	syncAggregate, err := block.Block().Body().SyncAggregate()
	if err != nil {
		return nil, &core.RpcError{Err: fmt.Errorf("Could not get sync aggregate: %v", err), Reason: core.Internal}
	}

	if syncAggregate.SyncCommitteeBits.Count() < config.MinSyncCommitteeParticipants {
		return nil, &core.RpcError{
			Err:    fmt.Errorf("Invalid sync committee bits count: %d", syncAggregate.SyncCommitteeBits.Count()),
			Reason: core.BadRequest,
		}
	}

	// assert state.slot == state.latest_block_header.slot
	if state.Slot() != state.LatestBlockHeader().Slot {
		return nil, &core.RpcError{Err: fmt.Errorf("Invalid state slot: %d", state.Slot()), Reason: core.BadRequest}
	}

	// assert hash_tree_root(header) == hash_tree_root(block.message)
	header := *state.LatestBlockHeader()
	stateRoot, err := state.HashTreeRoot(ctx)
	if err != nil {
		return nil, &core.RpcError{Err: fmt.Errorf("Could not get state root: %v", err), Reason: core.Internal}
	}
	header.StateRoot = stateRoot[:]

	headerRoot, err := header.HashTreeRoot()
	if err != nil {
		return nil, &core.RpcError{Err: fmt.Errorf("Could not get header root: %v", err), Reason: core.Internal}
	}

	blockRoot, err := block.Block().HashTreeRoot()
	if err != nil {
		return nil, &core.RpcError{Err: fmt.Errorf("Could not get block root: %v", err), Reason: core.Internal}
	}

	if headerRoot != blockRoot {
		return nil, &core.RpcError{Err: fmt.Errorf("Invalid header root: %v", headerRoot), Reason: core.BadRequest}
	}

	// assert attested_state.slot == attested_state.latest_block_header.slot
	if attestedState.Slot() != attestedState.LatestBlockHeader().Slot {
		return nil, &core.RpcError{Err: fmt.Errorf("Invalid attested state slot: %d", attestedState.Slot()), Reason: core.BadRequest}
	}

	// attested_header = attested_state.latest_block_header.copy()
	attestedHeader := *attestedState.LatestBlockHeader()

	// attested_header.state_root = hash_tree_root(attested_state)
	attestedStateRoot, err := attestedState.HashTreeRoot(ctx)
	if err != nil {
		return nil, &core.RpcError{Err: fmt.Errorf("Could not get attested state root: %v", err), Reason: core.Internal}
	}
	attestedHeader.StateRoot = attestedStateRoot[:]

	// assert hash_tree_root(attested_header) == block.message.parent_root
	attestedHeaderRoot, err := attestedHeader.HashTreeRoot()
	if err != nil || attestedHeaderRoot != block.Block().ParentRoot() {
		return nil, &core.RpcError{Err: fmt.Errorf("Invalid attested header root: %v", attestedHeaderRoot), Reason: core.BadRequest}
	}

	// Return result
	attestedHeaderResult := &ethpbv1.BeaconBlockHeader{
		Slot:          attestedHeader.Slot,
		ProposerIndex: attestedHeader.ProposerIndex,
		ParentRoot:    attestedHeader.ParentRoot,
		StateRoot:     attestedHeader.StateRoot,
		BodyRoot:      attestedHeader.BodyRoot,
	}

	syncAggregateResult := &ethpbv1.SyncAggregate{
		SyncCommitteeBits:      syncAggregate.SyncCommitteeBits,
		SyncCommitteeSignature: syncAggregate.SyncCommitteeSignature,
	}

	result := &ethpbv2.LightClientUpdate{
		AttestedHeader: attestedHeaderResult,
		SyncAggregate:  syncAggregateResult,
		SignatureSlot:  block.Block().Slot(),
	}

	return result, nil
}

func NewLightClientFinalityUpdateFromBeaconState(
	ctx context.Context,
	config *params.BeaconChainConfig,
	state state.BeaconState,
	block interfaces.ReadOnlySignedBeaconBlock,
	attestedState state.BeaconState,
	finalizedBlock interfaces.ReadOnlySignedBeaconBlock) (*ethpbv2.LightClientUpdate, *core.RpcError) {
	result, err := NewLightClientOptimisticUpdateFromBeaconState(
		ctx,
		config,
		state,
		block,
		attestedState,
	)
	if err != nil {
		return nil, err
	}

	// Indicate finality whenever possible
	var finalizedHeader *ethpbv1.BeaconBlockHeader
	var finalityBranch [][]byte

	if finalizedBlock != nil && !finalizedBlock.IsNil() {
		if finalizedBlock.Block().Slot() != 0 {
			tempFinalizedHeader, err := finalizedBlock.Header()
			if err != nil {
				return nil, &core.RpcError{Err: fmt.Errorf("Could not get finalized header: %v", err), Reason: core.Internal}
			}
			finalizedHeader = migration.V1Alpha1SignedHeaderToV1(tempFinalizedHeader).GetMessage()

			finalizedHeaderRoot, err := finalizedHeader.HashTreeRoot()
			if err != nil {
				return nil, &core.RpcError{Err: fmt.Errorf("Could not get finalized header root: %v", err), Reason: core.Internal}
			}

			if finalizedHeaderRoot != bytesutil.ToBytes32(attestedState.FinalizedCheckpoint().Root) {
				return nil, &core.RpcError{Err: fmt.Errorf("Invalid finalized header root: %v", finalizedHeaderRoot), Reason: core.BadRequest}
			}
		} else {
			if !bytes.Equal(attestedState.FinalizedCheckpoint().Root, make([]byte, 32)) {
				return nil, &core.RpcError{Err: fmt.Errorf("Invalid finalized header root: %v", attestedState.FinalizedCheckpoint().Root), Reason: core.BadRequest}
			}

			finalizedHeader = &ethpbv1.BeaconBlockHeader{
				Slot:          0,
				ProposerIndex: 0,
				ParentRoot:    make([]byte, 32),
				StateRoot:     make([]byte, 32),
				BodyRoot:      make([]byte, 32),
			}
		}

		var bErr error
		finalityBranch, bErr = attestedState.FinalizedRootProof(ctx)
		if bErr != nil {
			return nil, &core.RpcError{Err: fmt.Errorf("Could not get finalized root proof: %v", bErr), Reason: core.Internal}
		}
	} else {
		finalizedHeader = &ethpbv1.BeaconBlockHeader{
			Slot:          0,
			ProposerIndex: 0,
			ParentRoot:    make([]byte, 32),
			StateRoot:     make([]byte, 32),
			BodyRoot:      make([]byte, 32),
		}

		finalityBranch = make([][]byte, 6)
		for i := 0; i < 6; i++ {
			finalityBranch[i] = make([]byte, 32)
		}
	}

	result.FinalizedHeader = finalizedHeader
	result.FinalityBranch = finalityBranch
	return result, nil
}

// NewLightClientFinalityUpdateFromUpdate - implements https://github.com/ethereum/consensus-specs/blob/3d235740e5f1e641d3b160c8688f26e7dc5a1894/specs/altair/light-client/full-node.md#create_light_client_finality_update
// def create_light_client_finality_update(update: LightClientUpdate) -> LightClientFinalityUpdate:
//
//	return LightClientFinalityUpdate(
//	    attested_header=update.attested_header,
//	    finalized_header=update.finalized_header,
//	    finality_branch=update.finality_branch,
//	    sync_aggregate=update.sync_aggregate,
//	    signature_slot=update.signature_slot,
//	)
func NewLightClientFinalityUpdateFromUpdate(update *ethpbv2.LightClientUpdate) *ethpbv2.LightClientFinalityUpdate {
	return &ethpbv2.LightClientFinalityUpdate{
		AttestedHeader:  update.AttestedHeader,
		FinalizedHeader: update.FinalizedHeader,
		FinalityBranch:  update.FinalityBranch,
		SyncAggregate:   update.SyncAggregate,
		SignatureSlot:   update.SignatureSlot,
	}
}

// NewLightClientOptimisticUpdateFromUpdate - implements https://github.com/ethereum/consensus-specs/blob/3d235740e5f1e641d3b160c8688f26e7dc5a1894/specs/altair/light-client/full-node.md#create_light_client_optimistic_update
// def create_light_client_optimistic_update(update: LightClientUpdate) -> LightClientOptimisticUpdate:
//
//	return LightClientOptimisticUpdate(
//	    attested_header=update.attested_header,
//	    sync_aggregate=update.sync_aggregate,
//	    signature_slot=update.signature_slot,
//	)
func NewLightClientOptimisticUpdateFromUpdate(update *ethpbv2.LightClientUpdate) *ethpbv2.LightClientOptimisticUpdate {
	return &ethpbv2.LightClientOptimisticUpdate{
		AttestedHeader: update.AttestedHeader,
		SyncAggregate:  update.SyncAggregate,
		SignatureSlot:  update.SignatureSlot,
	}
}

func NewLightClientUpdateFromFinalityUpdate(update *ethpbv2.LightClientFinalityUpdate) *ethpbv2.LightClientUpdate {
	return &ethpbv2.LightClientUpdate{
		AttestedHeader:  update.AttestedHeader,
		FinalizedHeader: update.FinalizedHeader,
		FinalityBranch:  update.FinalityBranch,
		SyncAggregate:   update.SyncAggregate,
		SignatureSlot:   update.SignatureSlot,
	}
}

func NewLightClientUpdateFromOptimisticUpdate(update *ethpbv2.LightClientOptimisticUpdate) *ethpbv2.LightClientUpdate {
	return &ethpbv2.LightClientUpdate{
		AttestedHeader: update.AttestedHeader,
		SyncAggregate:  update.SyncAggregate,
		SignatureSlot:  update.SignatureSlot,
	}
}
