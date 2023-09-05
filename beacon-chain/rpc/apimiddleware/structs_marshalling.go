package apimiddleware

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

const (
	LightClientUpdateTypeName           = "light_client_update"
	LightClientFinalityUpdateTypeName   = "light_client_finality_update"
	LightClientOptimisticUpdateTypeName = "light_client_optimistic_update"
)

// EpochParticipation represents participation of validators in their duties.
type EpochParticipation []string

func (p *EpochParticipation) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) < 2 {
		return errors.New("epoch participation length must be at least 2")
	}
	if b[0] != '"' || b[len(b)-1] != '"' {
		return errors.Errorf("provided epoch participation json string is malformed: %s", string(b))
	}

	// Remove leading and trailing quotation marks.
	jsonString := string(b)
	jsonString = strings.Trim(jsonString, "\"")
	decoded, err := base64.StdEncoding.DecodeString(jsonString)
	if err != nil {
		return errors.Wrapf(err, "could not decode epoch participation base64 value")
	}

	*p = make([]string, len(decoded))
	for i, participation := range decoded {
		(*p)[i] = strconv.FormatUint(uint64(participation), 10)
	}
	return nil
}

// TypedLightClientUpdateJson is a wrapper around updates of specific types. It allows callers to send typed updates
// over a single endpoint while preserving strict typing.
type TypedLightClientUpdateJson struct {
	// TypeName is the name of the update type
	TypeName string `json:"type_name"`
	// Data is the update of type TypeName marshaled as JSON
	Data string `json:"data"`
}

func NewTypedLightClientUpdateJsonFromUpdate(update *LightClientUpdateJson) (*TypedLightClientUpdateJson, error) {
	bytes, err := json.Marshal(update)
	if err != nil {
		return nil, err
	}
	return &TypedLightClientUpdateJson{
		TypeName: LightClientUpdateTypeName,
		Data:     string(bytes),
	}, nil
}

func NewTypedLightClientUpdateJsonFromFinalityUpdate(update *LightClientFinalityUpdateJson) (
	*TypedLightClientUpdateJson,
	error) {
	bytes, err := json.Marshal(update)
	if err != nil {
		return nil, err
	}
	return &TypedLightClientUpdateJson{
		TypeName: LightClientFinalityUpdateTypeName,
		Data:     string(bytes),
	}, nil
}

func NewTypedLightClientUpdateJsonFromOptimisticUpdate(update *LightClientOptimisticUpdateJson) (
	*TypedLightClientUpdateJson,
	error) {
	bytes, err := json.Marshal(update)
	if err != nil {
		return nil, err
	}
	return &TypedLightClientUpdateJson{
		TypeName: LightClientOptimisticUpdateTypeName,
		Data:     string(bytes),
	}, nil
}
