package evr

import (
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
)

var (
	_ = IdentifyingMessage(&LobbyCreateSessionRequest{})
)

// LobbyCreateSessionRequest represents a request from the client to the server for creating a new game session.
type LobbyCreateSessionRequest struct {
	Region           Symbol          // Symbol representing the region
	VersionLock      Symbol          // Version lock
	Mode             Symbol          // Symbol representing the game type
	Level            Symbol          // Symbol representing the level
	Platform         Symbol          // Symbol representing the platform
	LoginSessionID   uuid.UUID       // Session identifier
	CrossPlayEnabled bool            // Whether cross-play is enabled
	LobbyType        LobbyType       // the visibility of the session to create.
	Unk2             uint32          // Unknown field 2
	GroupID          uuid.UUID       // Channel UUID
	SessionSettings  SessionSettings // Session settings
	Entrants         []Entrant
}

func (m *LobbyCreateSessionRequest) Stream(s *Stream) error {
	flags := uint32(0)

	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Region) },
		func() error { return s.Stream(&m.VersionLock) },
		func() error { return s.Stream(&m.Mode) },
		func() error { return s.Stream(&m.Level) },
		func() error { return s.Stream(&m.Platform) },
		func() error { return s.StreamUUID(&m.LoginSessionID) },
		func() error {
			c := int8(len(m.Entrants))
			if err := s.Stream(&c); err != nil {
				return err
			}
			if s.r != nil {
				m.Entrants = make([]Entrant, c) // Limit to 16
			}
			return s.Skip(7) // Alignment
		},
		func() error {
			lt := uint8(m.LobbyType)
			if err := s.Stream(&lt); err != nil {
				m.LobbyType = LobbyType(lt)
			}
			return s.Skip(3) // Alignment
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
			return nil
		},
		func() error { return s.Stream(&m.GroupID) },
		func() error {
			return s.StreamJSON(&m.SessionSettings, true, NoCompression)
		},
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
			if flags&SessionFlag_TeamIndexes != 0 && s.Len() >= len(m.Entrants) {
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
func (m *LobbyCreateSessionRequest) String() string {
	entrantstrs := make([]string, len(m.Entrants))
	for i, entrant := range m.Entrants {
		entrantstrs[i] = entrant.String()
	}

	return fmt.Sprintf("%T(RegionSymbol=%d, version_lock=%d, game_type=%d, level=%d, platform=%d, session=%s, lobby_type=%d, unk2=%d, channel=%s, session_settings=%s, entrants=%s)",
		m,
		m.Region,
		m.VersionLock,
		m.Mode,
		m.Level,
		m.Platform,
		m.LoginSessionID.String(),

		m.LobbyType,
		m.Unk2,
		m.GroupID.String(),
		m.SessionSettings.String(),
		strings.Join(entrantstrs, ", "),
	)
}

func (m *LobbyCreateSessionRequest) GetLoginSessionID() uuid.UUID {
	return m.LoginSessionID
}

func (m *LobbyCreateSessionRequest) GetEvrID() EvrID {
	if len(m.Entrants) == 0 {
		return EvrID{}
	}
	return m.Entrants[0].EvrID
}

func (m *LobbyCreateSessionRequest) GetChannel() uuid.UUID {
	return m.GroupID
}

func (m *LobbyCreateSessionRequest) GetMode() Symbol {
	return m.Mode
}

func (m *LobbyCreateSessionRequest) GetAlignment() int8 {
	if len(m.Entrants) == 0 {
		return int8(TeamUnassigned)
	}
	return int8(m.Entrants[0].Role)
}
