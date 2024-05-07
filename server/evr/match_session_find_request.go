package evr

import (
	"encoding/binary"
	"fmt"

	"github.com/gofrs/uuid/v5"
)

var _ = IdentifyingMessage(LobbyFindSessionRequest{})

// LobbyFindSessionRequest is a message from client to server requesting finding of an existing game session that
// matches the message's underlying arguments.
type LobbyFindSessionRequest struct {
	VersionLock      uint64
	Mode             Symbol
	Level            Symbol
	Platform         Symbol // DMO, OVR_ORG, etc.
	LoginSessionID   uuid.UUID
	CrossPlayEnabled bool

	CurrentMatch    uuid.UUID
	Channel         uuid.UUID
	SessionSettings SessionSettings
	Entrants        []Entrant
}

type Entrant struct {
	EvrID     EvrId
	TeamIndex int16 // -1 for any team
}

func (m LobbyFindSessionRequest) Token() string {
	return "SNSLobbyFindSessionRequestv11"
}

func (m *LobbyFindSessionRequest) Symbol() Symbol {
	return SymbolOf(m)
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
			switch s.Mode {
			case DecodeMode:

				if err := s.StreamNumber(binary.LittleEndian, &flags); err != nil {
					return err
				}

				m.CrossPlayEnabled = flags&0x200 != 0

			case EncodeMode:
				if m.CrossPlayEnabled {
					flags |= 0x200
				}

				flags = (flags & 0xFFFFFF00) | uint32(len(m.Entrants))

				// TeamIndexes are only sent if there are entrants with team indexes > -1
				for _, entrant := range m.Entrants {
					if entrant.TeamIndex > -1 {
						flags |= 0x100
						break
					}
				}
				return s.StreamNumber(binary.LittleEndian, &flags)
			}
			return nil
		},
		func() error { return s.Skip(4) },
		func() error { return s.StreamGuid(&m.CurrentMatch) },
		func() error { return s.StreamGuid(&m.Channel) },
		func() error { return s.StreamJson(&m.SessionSettings, true, NoCompression) },
		func() error {
			// Stream the entrants
			if s.Mode == DecodeMode {
				m.Entrants = make([]Entrant, flags&0xFF)
			}

			for i := range m.Entrants {
				if err := s.StreamStruct(&m.Entrants[i].EvrID); err != nil {
					return err
				}
			}

			return nil
		},
		func() error {
			// Stream the team indexes
			if flags&0x100 != 0 {

				for i := range m.Entrants {
					if err := s.StreamNumber(binary.LittleEndian, &m.Entrants[i].TeamIndex); err != nil {
						return err
					}
				}

			} else if s.Mode == DecodeMode {
				// Set all the team indexes to -1 (any)
				for i := range m.Entrants {
					m.Entrants[i].TeamIndex = -1
				}

			}

			return nil
		},
	})

}

func (m LobbyFindSessionRequest) String() string {
	return fmt.Sprintf("LobbyFindSessionRequest{VersionLock: %d, Mode: %d, Level: %d, Platform: %d, LoginSessionID: %s, CrossPlayEnabled: %t, CurrentMatch: %s, Channel: %s, SessionSettings: %v, Entrants: %v}", m.VersionLock, m.Mode, m.Level, m.Platform, m.LoginSessionID, m.CrossPlayEnabled, m.CurrentMatch, m.Channel, m.SessionSettings, m.Entrants)
}

type Flags struct {
	PlayersInParty   [8]byte
	HasTeams         bool // 1 bit
	CrossPlayEnabled bool // 1 bit
}

func NewFindSessionRequest(versionLock uint64, mode Symbol, level Symbol, platform Symbol, loginSessionID uuid.UUID, crossPlayEnabled bool, currentMatch uuid.UUID, channel uuid.UUID, sessionSettings SessionSettings, entrants []Entrant) LobbyFindSessionRequest {
	return LobbyFindSessionRequest{
		VersionLock:      versionLock,
		Mode:             mode,
		Level:            level,
		Platform:         platform,
		LoginSessionID:   loginSessionID,
		CrossPlayEnabled: crossPlayEnabled,
		CurrentMatch:     currentMatch,
		Channel:          channel,
		SessionSettings:  sessionSettings,
		Entrants:         entrants,
	}
}

func (m LobbyFindSessionRequest) GetSessionID() uuid.UUID {
	return m.LoginSessionID
}

func (m LobbyFindSessionRequest) GetEvrID() EvrId {
	if len(m.Entrants) == 0 {
		return EvrId{}
	}

	return m.Entrants[0].EvrID
}
