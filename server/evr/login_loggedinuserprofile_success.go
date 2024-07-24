package evr

import (
	"fmt"
)

// SNSLoggedInUserProfileResponse is a message from client to
// server requesting the user profile for their logged-in account.
type LoggedInUserProfileSuccess struct {
	UserId  EvrID
	Payload GameProfiles
}

func (m *LoggedInUserProfileSuccess) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.UserId.PlatformCode) },
		func() error { return s.Stream(&m.UserId.AccountID) },
		func() error { return s.StreamJSON(&m.Payload, true, ZstdCompression) },
	})
}
func (m LoggedInUserProfileSuccess) String() string {
	return fmt.Sprintf("%T(user_id=%v)", m, m.UserId)
}

func NewLoggedInUserProfileSuccess(userId EvrID, client ClientProfile, server ServerProfile) *LoggedInUserProfileSuccess {
	return &LoggedInUserProfileSuccess{
		UserId: userId,
		Payload: GameProfiles{
			Client: client,
			Server: server,
		},
	}
}
