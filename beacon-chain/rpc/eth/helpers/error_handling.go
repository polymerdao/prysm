package helpers

import (
	"errors"
	"fmt"

	"github.com/prysmaticlabs/prysm/v4/beacon-chain/rpc/lookup"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/state/stategen"
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
