package evr

import (
	"fmt"
)

// SNSLobbySmiteEntrant represents a message from server to client indicating a failure in OtherUserProfileRequest.
type SNSLobbySmiteEntrant struct {
	Slot int64
}

func NewSNSLobbySmiteEntrant(slot int64) *SNSLobbySmiteEntrant {
	return &SNSLobbySmiteEntrant{
		Slot: slot,
	}
}

func (m *SNSLobbySmiteEntrant) Stream(s *Stream) error {
	return s.Stream(&m.Slot)
}

func (m *SNSLobbySmiteEntrant) String() string {
	return fmt.Sprintf("%T(slot=%d)", m, m.Slot)
}
