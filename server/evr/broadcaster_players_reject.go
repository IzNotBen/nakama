package evr

import (
	"fmt"
	"strings"
)

const (
	PlayerRejectionReasonInternal         PlayerRejectionReason = iota // Internal server error
	PlayerRejectionReasonBadRequest                                    // Bad request from the player
	PlayerRejectionReasonTimeout                                       // Player connection timeout
	PlayerRejectionReasonDuplicate                                     // Duplicate player session
	PlayerRejectionReasonLobbyLocked                                   // Lobby is locked
	PlayerRejectionReasonLobbyFull                                     // Lobby is full
	PlayerRejectionReasonLobbyEnding                                   // Lobby is ending
	PlayerRejectionReasonKickedFromServer                              // Player was kicked from the server
	PlayerRejectionReasonDisconnected                                  // Player was disconnected
	PlayerRejectionReasonInactive                                      // Player is inactive
)

type PlayerRejectionReason byte

// Nakama -> Game Server: player sessions that are to be kicked/rejected.
type BroadcasterPlayersReject struct {
	ErrorCode      byte
	PlayerSessions []GUID
}

// NewGameServerPlayersRejected initializes a new GameServerPlayersRejected message with the provided arguments.
func NewBroadcasterPlayersReject(errorCode PlayerRejectionReason, playerSessions ...GUID) *BroadcasterPlayersReject {
	return &BroadcasterPlayersReject{
		ErrorCode:      byte(errorCode),
		PlayerSessions: playerSessions,
	}
}

func (m *BroadcasterPlayersReject) Stream(s *Stream) error {
	if s.Mode == DecodeMode {
		m.PlayerSessions = make([]GUID, (s.r.Len()-1)/16)
	}
	return s.Stream([]Streamer{
		&m.ErrorCode,
		m.PlayerSessions,
	})
}

func (m *BroadcasterPlayersReject) String() string {
	sessions := make([]string, len(m.PlayerSessions))
	for i, s := range m.PlayerSessions {
		sessions[i] = s.String()
	}
	return fmt.Sprintf("BroadcasterPlayersRejected(error_code=%v, player_sessions=[%s])", m.ErrorCode, strings.Join(sessions, ", "))
}
