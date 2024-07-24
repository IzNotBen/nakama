package evr

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/lo"
)

// LobbyPlayerSessionsRequest is a message from client to server, asking it to obtain game server sessions for a given list of user identifiers.
type LobbyPlayerSessionsRequest struct {
	LoginSessionID uuid.UUID
	EvrID          EvrID
	LobbyID        uuid.UUID
	Platform       Symbol
	PlayerEvrIDs   []EvrID
}

func (m LobbyPlayerSessionsRequest) Token() string {
	return "SNSLobbyPlayerSessionsRequestv5"
}

func (m LobbyPlayerSessionsRequest) Symbol() Symbol {
	return SymbolOf(&m)
}

func (m *LobbyPlayerSessionsRequest) Stream(s *EasyStream) error {
	playerCount := uint64(len(m.PlayerEvrIDs))
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(&m.LoginSessionID) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.AccountID) },
		func() error { return s.StreamGuid(&m.LobbyID) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Platform) },
		func() error { return s.StreamNumber(binary.LittleEndian, &playerCount) },
		func() error {
			if s.r != nil {
				m.PlayerEvrIDs = make([]EvrID, playerCount)
			}
			for i := range m.PlayerEvrIDs {
				if err := s.StreamStruct(&m.PlayerEvrIDs[i]); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func (m *LobbyPlayerSessionsRequest) String() string {
	evrIDstrs := strings.Join(lo.Map(m.PlayerEvrIDs, func(id EvrID, i int) string { return id.Token() }), ", ")
	return fmt.Sprintf("%T(login_session_id=%s, evr_id=%s, lobby_id=%s, evr_ids=%s)", m, m.LoginSessionID, m.EvrID, m.LobbyID, evrIDstrs)
}

func (m *LobbyPlayerSessionsRequest) GetSessionID() uuid.UUID {
	return m.LoginSessionID
}

func (m *LobbyPlayerSessionsRequest) GetEvrID() EvrID {
	return m.EvrID
}

func (m *LobbyPlayerSessionsRequest) MatchSessionID() uuid.UUID {
	return m.LobbyID
}
