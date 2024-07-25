package evr

import (
	"encoding/json"
	"fmt"
)

// OtherUserProfileSuccess is a message from server to the client indicating a successful OtherUserProfileRequest.
// It contains profile information about the requested user.
type OtherUserProfileSuccess struct {
	Message
	EvrID             EvrID
	ServerProfileJSON []byte
}

func NewOtherUserProfileSuccess(evrID EvrID, profile *ServerProfile) *OtherUserProfileSuccess {
	data, err := json.Marshal(profile)
	if err != nil {
		panic("failed to marshal profile")
	}

	return &OtherUserProfileSuccess{
		EvrID:             evrID,
		ServerProfileJSON: data,
	}
}

func (m *OtherUserProfileSuccess) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.EvrID) },
		func() error {
			return s.StreamCompressedBytes(&m.ServerProfileJSON, true, ZstdCompression)
		},
	})
}

func (m *OtherUserProfileSuccess) String() string {
	return fmt.Sprintf("%T(user_id=%s)", m, m.EvrID.String())
}

func (m *OtherUserProfileSuccess) GetProfile() ServerProfile {
	profile := ServerProfile{}
	err := json.Unmarshal(m.ServerProfileJSON, &profile)
	if err != nil {
		panic("failed to unmarshal profile")
	}
	return profile
}
