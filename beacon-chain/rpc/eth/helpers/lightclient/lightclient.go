package lightclient

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/prysmaticlabs/prysm/v4/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/v4/config/params"
	ethpbv1 "github.com/prysmaticlabs/prysm/v4/proto/eth/v1"
	ethpbv2 "github.com/prysmaticlabs/prysm/v4/proto/eth/v2"
	"github.com/prysmaticlabs/prysm/v4/time/slots"
)

// NewLightClientBootstrapFromBeaconState - implements https://github.com/ethereum/consensus-specs/blob/3d235740e5f1e641d3b160c8688f26e7dc5a1894/specs/altair/light-client/full-node.md#create_light_client_bootstrap
// def create_light_client_bootstrap(state: BeaconState) -> LightClientBootstrap:
//
//	assert compute_epoch_at_slot(state.slot) >= ALTAIR_FORK_EPOCH
//	assert state.slot == state.latest_block_header.slot
//
//	return LightClientBootstrap(
//	    header=BeaconBlockHeader(
//	        slot=state.latest_block_header.slot,
//	        proposer_index=state.latest_block_header.proposer_index,
//	        parent_root=state.latest_block_header.parent_root,
//	        state_root=hash_tree_root(state),
//	        body_root=state.latest_block_header.body_root,
//	    ),
//	    current_sync_committee=state.current_sync_committee,
//	    current_sync_committee_branch=compute_merkle_proof_for_state(state, CURRENT_SYNC_COMMITTEE_INDEX)
//	)
func NewLightClientBootstrapFromBeaconState(ctx context.Context, state state.BeaconState) (*ethpbv2.LightClientBootstrap, error) {
	// assert compute_epoch_at_slot(state.slot) >= ALTAIR_FORK_EPOCH
	if slots.ToEpoch(state.Slot()) < params.BeaconConfig().AltairForkEpoch {
		return nil, status.Errorf(codes.Internal, "Invalid state slot: %d", state.Slot())
	}

	// assert state.slot == state.latest_block_header.slot
	if state.Slot() != state.LatestBlockHeader().Slot {
		return nil, status.Errorf(codes.Internal, "Invalid state slot: %d", state.Slot())
	}

	// Prepare data
	latestBlockHeader := state.LatestBlockHeader()

	currentSyncCommittee, err := state.CurrentSyncCommittee()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get current sync committee: %v", err)
	}

	committee := ethpbv2.SyncCommittee{
		Pubkeys:         currentSyncCommittee.GetPubkeys(),
		AggregatePubkey: currentSyncCommittee.GetAggregatePubkey(),
	}

	branch, err := state.CurrentSyncCommitteeProof(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get current sync committee proof: %v", err)
	}

	stateRoot, err := state.HashTreeRoot(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get state root: %v", err)
	}

	// Return result
	result := &ethpbv2.LightClientBootstrap{
		Header: &ethpbv1.BeaconBlockHeader{
			Slot:          latestBlockHeader.Slot,
			ProposerIndex: latestBlockHeader.ProposerIndex,
			ParentRoot:    latestBlockHeader.ParentRoot,
			StateRoot:     stateRoot[:],
			BodyRoot:      latestBlockHeader.BodyRoot,
		},
		CurrentSyncCommittee:       &committee,
		CurrentSyncCommitteeBranch: branch,
	}

	return result, nil
}

