package beacon

import (
	"context"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lightclienthelpers "github.com/prysmaticlabs/prysm/v4/beacon-chain/rpc/eth/helpers/lightclient"
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

