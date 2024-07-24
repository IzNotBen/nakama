package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

// client -> nakama: request the user profile for their logged-in account.
type LoggedInUserProfileRequest struct {
	Session            uuid.UUID
	EvrID              EvrID
	ProfileRequestData ProfileRequestData
}

func (m LoggedInUserProfileRequest) String() string {
	return fmt.Sprintf("%T(session=%v, user_id=%v, profile_request=%v)", m, m.Session, m.EvrID, m.ProfileRequestData)
}

func (m *LoggedInUserProfileRequest) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Session) },
		func() error { return s.Stream(&m.EvrID.PlatformCode) },
		func() error { return s.Stream(&m.EvrID.AccountID) },
		func() error { return s.StreamJSON(&m.ProfileRequestData, true, NoCompression) },
	})
}

func NewLoggedInUserProfileRequest(session uuid.UUID, evrID EvrID, profileRequestData ProfileRequestData) LoggedInUserProfileRequest {
	return LoggedInUserProfileRequest{
		Session:            session,
		EvrID:              evrID,
		ProfileRequestData: profileRequestData,
	}
}
func (m *LoggedInUserProfileRequest) GetLoginSessionID() uuid.UUID {
	return m.Session
}

func (m *LoggedInUserProfileRequest) GetEvrID() EvrID {
	return m.EvrID
}

type ProfileRequestData struct {
	Defaultclientprofileid string       `json:"defaultclientprofileid"`
	Defaultserverprofileid string       `json:"defaultserverprofileid"`
	Unlocksetids           Unlocksetids `json:"unlocksetids"`
	Statgroupids           Statgroupids `json:"statgroupids"`
}

type Statgroupids struct {
	Arena           map[string]interface{} `json:"arena"`
	ArenaPracticeAI map[string]interface{} `json:"arena_practice_ai"`
	ArenaPublicAI   map[string]interface{} `json:"arena_public_ai"`
	Combat          map[string]interface{} `json:"combat"`
}

type Unlocksetids struct {
	All map[string]interface{} `json:"all"`
}
