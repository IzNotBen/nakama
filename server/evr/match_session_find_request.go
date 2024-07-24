package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

const (
	// First 8 bytes are the entrant count
	SessionFlag_TeamIndexes uint32 = 1 << iota
	SessionFlag_EnableCrossPlay
)

var (
	_ = IdentifyingMessage(&LobbyFindSessionRequest{})
)

type LobbyFindSessionRequest struct {
	LobbyID     uuid.UUID
	VersionLock Symbol
	Mode        Symbol
	Level       Symbol
	Platform    Symbol

	SessionSettings SessionSettings

	CrossPlayEnabled bool

	GroupID        uuid.UUID
	CurrentLobbyID uuid.UUID
	LoginSessionID uuid.UUID

	Entrants []Entrant
}

func NewLobbyFindSessionRequest(versionLock Symbol, mode Symbol, level Symbol, platform Symbol, loginSessionID uuid.UUID, crossPlayEnabled bool, currentMatch uuid.UUID, channel uuid.UUID, sessionSettings SessionSettings, entrants []Entrant) LobbyFindSessionRequest {
	return LobbyFindSessionRequest{
		VersionLock:      versionLock,
		Mode:             mode,
		Level:            level,
		Platform:         platform,
		LoginSessionID:   loginSessionID,
		CrossPlayEnabled: crossPlayEnabled,
		CurrentLobbyID:   currentMatch,
		GroupID:          channel,
		SessionSettings:  sessionSettings,
		Entrants:         entrants,
	}
}

func (m LobbyFindSessionRequest) String() string {
	return fmt.Sprintf("%T{Mode: %s, Level: %s, Channel: %s}", m, m.Mode, m.Level, m.GroupID)

}

func (m *LobbyFindSessionRequest) Stream(s *Stream) error {
	flags := uint32(0)
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.VersionLock) },
		func() error { return s.Stream(&m.Mode) },
		func() error { return s.Stream(&m.Level) },
		func() error { return s.Stream(&m.Platform) },
		func() error { return s.Stream(&m.LoginSessionID) },
		func() error {
			c := uint8(len(m.Entrants))
			if err := s.Stream(&c); err != nil {
				return err
			}
			if s.r != nil {
				m.Entrants = make([]Entrant, c)
			}
			return nil
		},
		func() error {
			if s.r != nil {

				if err := s.Stream(&flags); err != nil {
					return err
				}
				// If there are no team indexes, set all of the Roles to -1 (unspecified)
				if flags&SessionFlag_TeamIndexes == 0 {
					for i := range m.Entrants {
						// Set all of the Roles to -1 (unspecified)
						m.Entrants[i].Role = -1
					}
				}

				m.CrossPlayEnabled = flags&SessionFlag_EnableCrossPlay != 0

			} else {

				// TeamIndexes are only sent if there are entrants with team indexes > -1
				for _, entrant := range m.Entrants {
					if entrant.Role > -1 {
						flags |= SessionFlag_TeamIndexes
						break
					}
				}
				if m.CrossPlayEnabled {
					flags |= SessionFlag_EnableCrossPlay
				}
				return s.Stream(&flags)
			}
			return s.Skip(3) // Skip 3 bytes for alignment
		},
		func() error { return s.Stream(&m.CurrentLobbyID) },
		func() error { return s.Stream(&m.GroupID) },
		func() error { return s.StreamJSON(&m.SessionSettings, true, NoCompression) },
		func() error {
			// Stream the entrants
			for i := range m.Entrants {
				if err := s.Stream(&m.Entrants[i].EvrID); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			// Stream the team indexes
			if flags&SessionFlag_TeamIndexes != 0 {
				for i := range m.Entrants {
					if err := s.Stream(&m.Entrants[i].Role); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})

}

func (m LobbyFindSessionRequest) GetGroupID() uuid.UUID { return m.GroupID }

func (m *LobbyFindSessionRequest) SetGroupID(groupID uuid.UUID) { m.GroupID = groupID }

func (m LobbyFindSessionRequest) GetMode() Symbol { return m.Mode }

func (m LobbyFindSessionRequest) GetLoginSessionID() uuid.UUID { return m.LoginSessionID }

func (m LobbyFindSessionRequest) GetEvrID() (evrID EvrID) {
	if len(m.Entrants) > 0 {
		return m.Entrants[0].EvrID
	}
	return evrID
}

func (m *LobbyFindSessionRequest) GetRoleAlignment() int {
	if len(m.Entrants) == 0 {
		return TeamUnassigned
	}
	return int(m.Entrants[0].Role)
}

func (m LobbyFindSessionRequest) GetCurrentLobbyID() uuid.UUID { return m.CurrentLobbyID }

func (m LobbyFindSessionRequest) GetEntrants() []Entrant { return m.Entrants }

func (m LobbyFindSessionRequest) GetLevel() Symbol { return m.Level }
