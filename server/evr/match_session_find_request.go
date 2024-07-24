package evr

import (
	"encoding/binary"
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
	_ = LobbySessionRequest(&LobbyFindSessionRequest{})
)

// LobbyFindSessionRequest is a message from client to server requesting finding of an existing game session that
// matches the message's underlying arguments.
type LobbyFindSessionRequest struct {
	VersionLock      Symbol
	Mode             Symbol
	Level            Symbol
	Platform         Symbol // DMO, OVR_ORG, etc.
	LoginSessionID   uuid.UUID
	CrossPlayEnabled bool

	CurrentLobbySessionID uuid.UUID
	GroupID               uuid.UUID
	SessionSettings       SessionSettings
	Entrants              []Entrant
}

func NewLobbyFindSessionRequest(versionLock Symbol, mode Symbol, level Symbol, platform Symbol, loginSessionID uuid.UUID, crossPlayEnabled bool, currentMatch uuid.UUID, channel uuid.UUID, sessionSettings SessionSettings, entrants []Entrant) LobbyFindSessionRequest {
	return LobbyFindSessionRequest{
		VersionLock:           versionLock,
		Mode:                  mode,
		Level:                 level,
		Platform:              platform,
		LoginSessionID:        loginSessionID,
		CrossPlayEnabled:      crossPlayEnabled,
		CurrentLobbySessionID: currentMatch,
		GroupID:               channel,
		SessionSettings:       sessionSettings,
		Entrants:              entrants,
	}
}

func (m LobbyFindSessionRequest) String() string {
	return fmt.Sprintf("LobbyFindSessionRequest{Mode: %s, Level: %s, Channel: %s}", m.Mode, m.Level, m.GroupID)

}

func (m *LobbyFindSessionRequest) Stream(s *EasyStream) error {
	flags := uint32(0)
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.VersionLock) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Mode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Level) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Platform) },
		func() error { return s.StreamGuid(&m.LoginSessionID) },
		func() error {
			c := uint8(len(m.Entrants))
			if err := s.StreamNumber(binary.LittleEndian, &c); err != nil {
				return err
			}
			if s.Mode == DecodeMode {
				m.Entrants = make([]Entrant, c)
			}
			return nil
		},
		func() error {
			switch s.Mode {
			case DecodeMode:

				if err := s.StreamNumber(binary.LittleEndian, &flags); err != nil {
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

			case EncodeMode:

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
				return s.StreamNumber(binary.LittleEndian, &flags)
			}
			return s.Skip(3) // Skip 3 bytes for alignment
		},
		func() error { return s.StreamGuid(&m.CurrentLobbySessionID) },
		func() error { return s.StreamGuid(&m.GroupID) },
		func() error { return s.StreamJson(&m.SessionSettings, true, NoCompression) },
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

func (m *LobbyFindSessionRequest) GetChannel() uuid.UUID { return m.GroupID }

func (m *LobbyFindSessionRequest) GetMode() Symbol { return m.Mode }

func (m LobbyFindSessionRequest) GetSessionID() uuid.UUID { return m.LoginSessionID }

func (m LobbyFindSessionRequest) GetEvrID() (evrID EvrID) {
	if len(m.Entrants) > 0 {
		return m.Entrants[0].EvrID
	}
	return evrID
}

func (m *LobbyFindSessionRequest) GetAlignment() int8 {
	if len(m.Entrants) == 0 {
		return int8(TeamUnassigned)
	}
	return m.Entrants[0].Role
}
