package evr

import (
	"fmt"
)

// BroadcasterPlayerRemoved is a message from broadcaster to nakama, indicating a player was removed by the game server.
// NOTE: This is an unofficial message created for Echo Relay.
type BroadcasterPlayerRemoved struct {
	PlayerSession GUID
}

func (m *BroadcasterPlayerRemoved) Symbol() Symbol {
	return SymbolOf(m)
}
func (m BroadcasterPlayerRemoved) Token() string {
	return "ERGameServerPlayerRemove"
}

// NewERGameServerRemovePlayer initializes a new ERGameServerRemovePlayer message.
func NewBroadcasterRemovePlayer(sid GUID) *BroadcasterPlayerRemoved {
	return &BroadcasterPlayerRemoved{
		PlayerSession: sid,
	}
}

func (m *BroadcasterPlayerRemoved) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGUID(&m.PlayerSession) },
	})
}
func (m *BroadcasterPlayerRemoved) String() string {

	return fmt.Sprintf("BroadcasterPlayerRemoved(player_session=%s)", m.PlayerSession.String())
}
