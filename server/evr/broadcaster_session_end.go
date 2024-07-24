package evr

import (
	"fmt"
)

// GameServerSessionEnded is a message from game server to server, indicating the game server's session ended.
// NOTE: This is an unofficial message created for Echo Relay.
type GameServerSessionEnded struct {
	Unused byte
}

func (m GameServerSessionEnded) String() string {
	return fmt.Sprintf("BroadcasterSessionEnded(unused=%d)", m.Unused)
}

func (m *GameServerSessionEnded) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamByte(&m.Unused) },
	})
}
