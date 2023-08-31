package beacon

import (
	"context"
	"math"

	"github.com/golang/protobuf/ptypes/empty"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lightclienthelpers "github.com/prysmaticlabs/prysm/v4/beacon-chain/rpc/eth/helpers/lightclient"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/v4/config/params"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/interfaces"
	types "github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
	ethpbv2 "github.com/prysmaticlabs/prysm/v4/proto/eth/v2"
)

// GetLightClientBootstrap - implements https://github.com/ethereum/beacon-APIs/blob/263f4ed6c263c967f13279c7a9f5629b51c5fc55/apis/beacon/light_client/bootstrap.yaml
func (bs *Server) GetLightClientBootstrap(ctx context.Context, req *ethpbv2.LightClientBootstrapRequest) (*ethpbv2.LightClientBootstrapResponse, error) {
	// Prepare
	ctx, span := trace.StartSpan(ctx, "beacon.GetLightClientBootstrap")
	defer span.End()

	// Get the block
	var blockRoot [32]byte
	copy(blockRoot[:], req.BlockRoot)

	blk, err := bs.BeaconDB.Block(ctx, blockRoot)
	err = handleGetBlockError(blk, err)
	if err != nil {
		return nil, err
	}

	// Get the state
	state, err := bs.Stater.StateBySlot(ctx, blk.Block().Slot())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get state by slot: %v", err)
	}

	bootstrap, err := lightclienthelpers.NewLightClientBootstrapFromBeaconState(ctx, state)
	if err != nil {
		return nil, err
	}

	result := &ethpbv2.LightClientBootstrapResponse{
		Version: ethpbv2.Version(blk.Version()),
		Data:    bootstrap,
	}

	return result, nil
}

// GetLightClientUpdatesByRange - implements https://github.com/ethereum/beacon-APIs/blob/263f4ed6c263c967f13279c7a9f5629b51c5fc55/apis/beacon/light_client/updates.yaml
func (bs *Server) GetLightClientUpdatesByRange(ctx context.Context, req *ethpbv2.LightClientUpdatesByRangeRequest) (*ethpbv2.LightClientUpdatesByRangeResponse, error) {
	// Prepare
	ctx, span := trace.StartSpan(ctx, "beacon.GetLightClientUpdatesByRange")
	defer span.End()

	// Determine slots per period
	config := params.BeaconConfig()
	slotsPerPeriod := uint64(config.EpochsPerSyncCommitteePeriod) * uint64(config.SlotsPerEpoch)

	// Adjust count based on configuration
	count := uint64(req.Count)
	if count > config.MaxRequestLightClientUpdates {
		count = config.MaxRequestLightClientUpdates
	}

	// Determine the start and end periods
	startPeriod := req.StartPeriod
	endPeriod := startPeriod + count - 1

	// The end of start period must be later than Altair fork epoch, otherwise, can not get the sync committee votes
	startPeriodEndSlot := (startPeriod+1)*slotsPerPeriod - 1
	if startPeriodEndSlot < uint64(config.AltairForkEpoch)*uint64(config.SlotsPerEpoch) {
		startPeriod = uint64(config.AltairForkEpoch) * uint64(config.SlotsPerEpoch) / slotsPerPeriod
	}

	headState, err := bs.HeadFetcher.HeadState(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get head state: %v", err)
	}

	lHeadSlot := uint64(headState.Slot())
	headPeriod := lHeadSlot / slotsPerPeriod
	if headPeriod < endPeriod {
		endPeriod = headPeriod
	}

	// Populate updates
	var updates []*ethpbv2.LightClientUpdateWithVersion
	for period := startPeriod; period <= endPeriod; period++ {
		// Get the last known state of the period,
		//    1. We wish the block has a parent in the same period if possible
		//	  2. We wish the block has a state in the same period
		lLastSlotInPeriod := period*slotsPerPeriod + slotsPerPeriod - 1
		if lLastSlotInPeriod > lHeadSlot {
			lLastSlotInPeriod = lHeadSlot
		}
		lFirstSlotInPeriod := period * slotsPerPeriod

		// Let's not use the first slot in the period, otherwise the attested header will be in previous period
		lFirstSlotInPeriod++

		var state state.BeaconState
		var block interfaces.ReadOnlySignedBeaconBlock
		for lSlot := lLastSlotInPeriod; lSlot >= lFirstSlotInPeriod; lSlot-- {
			state, err = bs.Stater.StateBySlot(ctx, types.Slot(lSlot))
			if err != nil {
				continue
			}

			// Get the block
			latestBlockHeader := *state.LatestBlockHeader()
			latestStateRoot, err := state.HashTreeRoot(ctx)
			if err != nil {
				continue
			}
			latestBlockHeader.StateRoot = latestStateRoot[:]
			blockRoot, err := latestBlockHeader.HashTreeRoot()
			if err != nil {
				continue
			}

			block, err = bs.BeaconDB.Block(ctx, blockRoot)
			if err != nil || block == nil {
				continue
			}

			syncAggregate, err := block.Block().Body().SyncAggregate()
			if err != nil || syncAggregate == nil {
				continue
			}

			if syncAggregate.SyncCommitteeBits.Count()*3 < config.SyncCommitteeSize*2 {
				// Not enough votes
				continue
			}

			break
		}

		if block == nil {
			// No valid block found for the period
			continue
		}

		// Get attested state
		attestedRoot := block.Block().ParentRoot()
		attestedBlock, err := bs.BeaconDB.Block(ctx, attestedRoot)
		if err != nil || attestedBlock == nil {
			continue
		}

		attestedSlot := attestedBlock.Block().Slot()
		attestedState, err := bs.Stater.StateBySlot(ctx, attestedSlot)
		if err != nil {
			continue
		}

		// Get finalized block
		var finalizedBlock interfaces.ReadOnlySignedBeaconBlock
		finalizedCheckPoint := attestedState.FinalizedCheckpoint()
		if finalizedCheckPoint != nil {
			finalizedRoot := bytesutil.ToBytes32(finalizedCheckPoint.Root)
			finalizedBlock, err = bs.BeaconDB.Block(ctx, finalizedRoot)
			if err != nil {
				finalizedBlock = nil
			}
		}

		update, err := lightclienthelpers.NewLightClientUpdateFromBeaconState(
			ctx,
			config,
			slotsPerPeriod,
			state,
			block,
			attestedState,
			finalizedBlock,
		)

		if err == nil {
			updates = append(updates, &ethpbv2.LightClientUpdateWithVersion{
				Version: ethpbv2.Version(attestedState.Version()),
				Data:    update,
			})
		}
	}

	if len(updates) == 0 {
		return nil, status.Errorf(codes.NotFound, "No updates found")
	}

	result := ethpbv2.LightClientUpdatesByRangeResponse{
		Updates: updates,
	}

	return &result, nil
}

// GetLightClientFinalityUpdate - implements https://github.com/ethereum/beacon-APIs/blob/263f4ed6c263c967f13279c7a9f5629b51c5fc55/apis/beacon/light_client/finality_update.yaml
func (bs *Server) GetLightClientFinalityUpdate(ctx context.Context,
	_ *empty.Empty) (*ethpbv2.LightClientFinalityUpdateResponse, error) {
	// Prepare
	ctx, span := trace.StartSpan(ctx, "beacon.GetLightClientFinalityUpdate")
	defer span.End()

	// Finality update needs super majority of sync committee signatures
	config := params.BeaconConfig()
	minSignatures := uint64(math.Ceil(float64(config.MinSyncCommitteeParticipants) * 2 / 3))

	block, err := bs.getLightClientEventBlock(ctx, minSignatures)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get block and state: %v", err)
	}

	state, err := bs.Stater.StateBySlot(ctx, block.Block().Slot())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get state: %v", err)
	}

	// Get attested state
	attestedRoot := block.Block().ParentRoot()
	attestedBlock, err := bs.BeaconDB.Block(ctx, attestedRoot)
	if err != nil || attestedBlock == nil {
		return nil, status.Errorf(codes.Internal, "Could not get attested block: %v", err)
	}

	attestedSlot := attestedBlock.Block().Slot()
	attestedState, err := bs.Stater.StateBySlot(ctx, attestedSlot)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get attested state: %v", err)
	}

	// Get finalized block
	var finalizedBlock interfaces.ReadOnlySignedBeaconBlock
	finalizedCheckPoint := attestedState.FinalizedCheckpoint()
	if finalizedCheckPoint != nil {
		finalizedRoot := bytesutil.ToBytes32(finalizedCheckPoint.Root)
		finalizedBlock, err = bs.BeaconDB.Block(ctx, finalizedRoot)
		if err != nil {
			finalizedBlock = nil
		}
	}

	update, err := lightclienthelpers.NewLightClientFinalityUpdateFromBeaconState(
		ctx,
		config,
		state,
		block,
		attestedState,
		finalizedBlock,
	)

	if err != nil {
		return nil, err
	}

	finalityUpdate := lightclienthelpers.NewLightClientFinalityUpdateFromUpdate(update)

	// Return the result
	result := &ethpbv2.LightClientFinalityUpdateResponse{
		Version: ethpbv2.Version(attestedState.Version()),
		Data:    finalityUpdate,
	}

	return result, nil
}

// getLightClientEventBlock - returns the block that should be used for light client events, which satisfies the minimum number of signatures from sync committee
func (bs *Server) getLightClientEventBlock(ctx context.Context, minSignaturesRequired uint64) (interfaces.ReadOnlySignedBeaconBlock, error) {
	// Get the current state
	state, err := bs.HeadFetcher.HeadState(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get head state: %v", err)
	}

	// Get the block
	latestBlockHeader := *state.LatestBlockHeader()
	stateRoot, err := state.HashTreeRoot(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get state root: %v", err)
	}
	latestBlockHeader.StateRoot = stateRoot[:]
	latestBlockHeaderRoot, err := latestBlockHeader.HashTreeRoot()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not get latest block header root: %v", err)
	}

	block, err := bs.BeaconDB.Block(ctx, latestBlockHeaderRoot)
	if err != nil || block == nil {
		return nil, status.Errorf(codes.Internal, "Could not get latest block: %v", err)
	}

	// Loop through the blocks until we find a block that has super majority of sync committee signatures (2/3)
	var numOfSyncCommitteeSignatures uint64
	if syncAggregate, err := block.Block().Body().SyncAggregate(); err == nil && syncAggregate != nil {
		numOfSyncCommitteeSignatures = syncAggregate.SyncCommitteeBits.Count()
	}

	for numOfSyncCommitteeSignatures < minSignaturesRequired {
		// Get the parent block
		parentRoot := block.Block().ParentRoot()
		block, err = bs.BeaconDB.Block(ctx, parentRoot)
		if err != nil || block == nil {
			return nil, status.Errorf(codes.Internal, "Could not get parent block: %v", err)
		}

		// Get the number of sync committee signatures
		numOfSyncCommitteeSignatures = 0
		if syncAggregate, err := block.Block().Body().SyncAggregate(); err == nil && syncAggregate != nil {
			numOfSyncCommitteeSignatures = syncAggregate.SyncCommitteeBits.Count()
		}
	}

	return block, nil
}
