package evr

import (
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
)

// Game Server -> Nakama: player sessions that the server intends to accept.

type GameServerEntrants struct {
	EntrantIDs []uuid.UUID
}

func NewGameServerEntrants(entrantIDs []uuid.UUID) *GameServerEntrants {
	return &GameServerEntrants{
		EntrantIDs: entrantIDs,
	}
}

func (m *GameServerEntrants) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error {
			if s.r != nil {
				m.EntrantIDs = make([]uuid.UUID, s.Len()/16)
			}
			return s.Stream(&m.EntrantIDs)
		},
	})
}

func (m *GameServerEntrants) String() string {
	sessions := make([]string, len(m.EntrantIDs))
	for i, session := range m.EntrantIDs {
		sessions[i] = session.String()
	}
	return fmt.Sprintf("%T(entrant_ids=[%s])", m, strings.Join(sessions, ", "))
}

type GameServerEntrantAccepted struct {
	EntrantIDs []uuid.UUID
}

func NewGameServerEntrantAccepted(entrantIDs ...uuid.UUID) *GameServerEntrantAccepted {
	return &GameServerEntrantAccepted{EntrantIDs: entrantIDs}
}

func (m *GameServerEntrantAccepted) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Skip(1) },
		func() error {
			if s.r != nil {
				m.EntrantIDs = make([]uuid.UUID, s.Len()/16)
			}
			return s.Stream(&m.EntrantIDs)
		},
	})
}

func (m *GameServerEntrantAccepted) String() string {
	sessions := make([]string, len(m.EntrantIDs))
	for i, session := range m.EntrantIDs {
		sessions[i] = session.String()
	}
	return fmt.Sprintf("%T(entrant_ids=[%s])", m, strings.Join(sessions, ", "))
}

type GameServerEntrantsReject struct {
	ErrorCode  EntrantRejectionReason
	EntrantIDs []uuid.UUID
}

type EntrantRejectionReason byte

const (
	PlayerRejectionReasonInternal         EntrantRejectionReason = iota // Internal server error
	PlayerRejectionReasonBadRequest                                     // Bad request from the player
	PlayerRejectionReasonTimeout                                        // Player connection timeout
	PlayerRejectionReasonDuplicate                                      // Duplicate player session
	PlayerRejectionReasonLobbyLocked                                    // Lobby is locked
	PlayerRejectionReasonLobbyFull                                      // Lobby is full
	PlayerRejectionReasonLobbyEnding                                    // Lobby is ending
	PlayerRejectionReasonKickedFromServer                               // Player was kicked from the server
	PlayerRejectionReasonDisconnected                                   // Player was disconnected
	PlayerRejectionReasonInactive                                       // Player is inactive
)

func NewGameServerEntrantsReject(errorCode EntrantRejectionReason, playerSessions ...uuid.UUID) *GameServerEntrantsReject {
	return &GameServerEntrantsReject{
		ErrorCode:  errorCode,
		EntrantIDs: playerSessions,
	}
}

// Stream encodes or decodes the GameServerPlayersRejected message.
func (m *GameServerEntrantsReject) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error {
			errorCode := byte(m.ErrorCode)
			if err := s.StreamByte(&errorCode); err != nil {
				return err
			}
			m.ErrorCode = EntrantRejectionReason(errorCode)
			return nil
		},
		func() error {
			if s.r != nil {
				m.EntrantIDs = make([]uuid.UUID, (s.r.Len()-s.Position())/16)
			}
			return s.Stream(&m.EntrantIDs)
		},
	})
}

func (m *GameServerEntrantsReject) String() string {
	sessions := make([]string, len(m.EntrantIDs))
	for i, session := range m.EntrantIDs {
		sessions[i] = session.String()
	}
	return fmt.Sprintf("%T(error_code=%v, player_sessions=[%s])", m, m.ErrorCode, strings.Join(sessions, ", "))
}

type GameServerEntrantRemoved struct {
	EntrantID uuid.UUID
}

func NewGameServerEntrantRemoved(sid uuid.UUID) *GameServerEntrantRemoved {
	return &GameServerEntrantRemoved{
		EntrantID: sid,
	}
}

func (m *GameServerEntrantRemoved) Stream(s *Stream) error {
	return s.Stream(&m.EntrantID)
}

func (m *GameServerEntrantRemoved) String() string {
	return fmt.Sprintf("%T(player_session=%s)", m, m.EntrantID.String())
}
