package helpers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/prysmaticlabs/prysm/v4/beacon-chain/rpc/lookup"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/state/stategen"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/interfaces"
	http2 "github.com/prysmaticlabs/prysm/v4/network/http"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PrepareStateFetchGRPCError returns an appropriate gRPC error based on the supplied argument.
// The argument error should be a result of fetching state.
func PrepareStateFetchGRPCError(err error) error {
	if errors.Is(err, stategen.ErrNoDataForSlot) {
		return status.Errorf(codes.NotFound, "lacking historical data needed to fulfill request")
	}
	if stateNotFoundErr, ok := err.(*lookup.StateNotFoundError); ok {
		return status.Errorf(codes.NotFound, "State not found: %v", stateNotFoundErr)
	}
	if parseErr, ok := err.(*lookup.StateIdParseError); ok {
		return status.Errorf(codes.InvalidArgument, "Invalid state ID: %v", parseErr)
	}
	return status.Errorf(codes.Internal, "Invalid state ID: %v", err)
}

// IndexedVerificationFailure represents a collection of verification failures.
type IndexedVerificationFailure struct {
	Failures []*SingleIndexedVerificationFailure `json:"failures"`
}

// SingleIndexedVerificationFailure represents an issue when verifying a single indexed object e.g. an item in an array.
type SingleIndexedVerificationFailure struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// PrepareStateFetchError returns an appropriate error based on the supplied argument.
// The argument error should be a result of fetching state.
func PrepareStateFetchError(err error) error {
	if errors.Is(err, stategen.ErrNoDataForSlot) {
		return errors.New("lacking historical data needed to fulfill request")
	}
	if stateNotFoundErr, ok := err.(*lookup.StateNotFoundError); ok {
		return fmt.Errorf("state not found: %v", stateNotFoundErr)
	}
	return fmt.Errorf("could not fetch state: %v", err)
}

func HandleGetBlockError(blk interfaces.ReadOnlySignedBeaconBlock, err error) error {
	if invalidBlockIdErr, ok := err.(*lookup.BlockIdParseError); ok {
		return status.Errorf(codes.InvalidArgument, "Invalid block ID: %v", invalidBlockIdErr)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "Could not get block from block ID: %v", err)
	}
	if err := blocks.BeaconBlockIsNil(blk); err != nil {
		return status.Errorf(codes.NotFound, "Could not find requested block: %v", err)
	}
	return nil
}

func HandleGetBlockErrorJson(blk interfaces.ReadOnlySignedBeaconBlock, err error) *http2.DefaultErrorJson {
	if errors.Is(err, lookup.BlockIdParseError{}) {
		return &http2.DefaultErrorJson{
			Message: "Invalid block ID: " + err.Error(),
			Code:    http.StatusBadRequest,
		}
	}
	if err != nil {
		return &http2.DefaultErrorJson{
			Message: "Could not get block from block ID: " + err.Error(),
			Code:    http.StatusInternalServerError,
		}
	}
	if err := blocks.BeaconBlockIsNil(blk); err != nil {
		return &http2.DefaultErrorJson{
			Message: "Could not find requested block: " + err.Error(),
			Code:    http.StatusNotFound,
		}
	}
	return nil
}
