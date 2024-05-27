package evr

import (
	"encoding/binary"
	"fmt"
	"strings"
)

var (
	_ = IdentifyingMessage(&LobbyCreateSessionRequest{})
	_ = LobbySessionRequest(&LobbyCreateSessionRequest{})
)

var (
	DefaultRegion = ToSymbol("default")
)

// LobbyCreateSessionRequest represents a request from the client to the server for creating a new game session.
type LobbyCreateSessionRequest struct {
	Region           Symbol          // Symbol representing the region
	VersionLock      Symbol          // Version lock
	Mode             Symbol          // Symbol representing the game type
	Level            Symbol          // Symbol representing the level
	Platform         Symbol          // Symbol representing the platform
	LoginSessionID   GUID            // Session identifier
	CrossPlayEnabled bool            // Whether cross-play is enabled
	LobbyType        LobbyType       // the visibility of the session to create.
	Unk2             uint32          // Unknown field 2
	Channel          GUID            // Channel UUID
	SessionSettings  SessionSettings // Session settings
	Entrants         []Entrant
}

func (m LobbyCreateSessionRequest) Token() string {
	return "SNSLobbyCreateSessionRequestv9"
}

func (m LobbyCreateSessionRequest) Symbol() Symbol {
	return 6456590782678944787
}

func (m *LobbyCreateSessionRequest) Stream(s *Stream) error {
	flags := uint32(0)
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Region) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.VersionLock) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Mode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Level) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Platform) },
		func() error { return s.StreamGUID(&m.LoginSessionID) },
		func() error {
			switch s.Mode {
			case DecodeMode:

				if err := s.StreamNumber(binary.LittleEndian, &flags); err != nil {
					return err
				}

				m.CrossPlayEnabled = flags&SessionFlag_EnableCrossPlay != 0

				m.Entrants = make([]Entrant, flags&0xFF)

				// Set all of the Roles to -1 (unspecified) by default
				for i := range m.Entrants {
					m.Entrants[i].Role = -1
				}
			case EncodeMode:
				if m.CrossPlayEnabled {
					flags |= SessionFlag_EnableCrossPlay
				}

				flags = (flags & 0xFFFFFF00) | uint32(len(m.Entrants))

				// TeamIndexes are only sent if there are entrants with team indexes > -1
				for _, entrant := range m.Entrants {
					if entrant.Role > -1 {
						flags |= SessionFlag_TeamIndexes
						break
					}
				}
				return s.StreamNumber(binary.LittleEndian, &flags)
			}
			return s.Skip(4)
		},
		func() error { return s.StreamNumber(binary.LittleEndian, &m.LobbyType) },

		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk2) },

		func() error { return s.StreamGUID(&m.Channel) },

		func() error {

			return s.StreamJSON(&m.SessionSettings, true, NoCompression)
		},
		func() error {
			// Stream the entrants
			for i := range m.Entrants {
				if err := s.StreamStruct(&m.Entrants[i].EvrID); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			// Stream the team indexes
			if flags&SessionFlag_TeamIndexes != 0 {
				for i := range m.Entrants {
					if err := s.StreamNumber(binary.LittleEndian, &m.Entrants[i].Role); err != nil {
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

	return fmt.Sprintf("%s(RegionSymbol=%d, version_lock=%d, game_type=%d, level=%d, platform=%d, session=%s, lobby_type=%d, unk2=%d, channel=%s, session_settings=%s, entrants=%s)",
		m.Token(),
		m.Region,
		m.VersionLock,
		m.Mode,
		m.Level,
		m.Platform,
		m.LoginSessionID.String(),

		m.LobbyType,
		m.Unk2,
		m.Channel.String(),
		m.SessionSettings.String(),
		strings.Join(entrantstrs, ", "),
	)
}

func (m *LobbyCreateSessionRequest) GetSessionID() GUID {
	return m.LoginSessionID
}

func (m *LobbyCreateSessionRequest) GetEvrID() EvrId {
	if len(m.Entrants) == 0 {
		return EvrId{}
	}
	return m.Entrants[0].EvrID
}

func (m *LobbyCreateSessionRequest) GetChannel() GUID {
	return m.Channel
}

func (m *LobbyCreateSessionRequest) GetMode() Symbol {
	return m.Mode
}
