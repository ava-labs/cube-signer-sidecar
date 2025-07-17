package signerserver

import (
	"encoding/json"

	"github.com/ava-labs/cube-signer-sidecar/api"
)

type tokenData struct {
	api.NewSessionResponse
	ID
	// save the rest of the data so that we don't lose data when overwriting the file
	RawData rawMessageMap `json:"-"`
}

type ID struct {
	OrgID  string `json:"org_id"`
	RoleID string `json:"role_id"`
}

// used for deserializing the keys but not the values
type rawMessageMap = map[string]json.RawMessage

func (t *tokenData) MarshalJSON() ([]byte, error) {
	sessionResponse, err := toRawData(t.NewSessionResponse)
	if err != nil {
		return nil, err
	}

	id, err := toRawData(t.ID)
	if err != nil {
		return nil, err
	}

	for k, v := range sessionResponse {
		t.RawData[k] = v
	}

	for k, v := range id {
		t.RawData[k] = v
	}

	return json.Marshal(t.RawData)
}

func toRawData(v any) (map[string]json.RawMessage, error) {
	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	rawData := make(map[string]json.RawMessage)
	if err := json.Unmarshal(bytes, &rawData); err != nil {
		return nil, err
	}

	return rawData, nil
}

func (t *tokenData) UnmarshalJSON(data []byte) error {
	var (
		NewSessionResponse api.NewSessionResponse
		id                 ID
	)

	if err := json.Unmarshal(data, &NewSessionResponse); err != nil {
		return err
	}

	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}

	rawData := make(rawMessageMap)

	if err := json.Unmarshal(data, &rawData); err != nil {
		return err
	}

	t.NewSessionResponse = NewSessionResponse
	t.ID = id
	t.RawData = rawData

	return nil
}

func (t *tokenData) toAuthData() *api.AuthData {
	return &api.AuthData{
		EpochNum:   t.SessionInfo.Epoch,
		EpochToken: t.SessionInfo.EpochToken,
		OtherToken: t.SessionInfo.RefreshToken,
	}
}
