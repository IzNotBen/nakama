package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

// StatusUpdateReason describes the reason for the status update in a LobbyStatusNotifyv2 message.
type StatusUpdateReason uint64

const (
	StatusUpdateBanned = StatusUpdateReason(iota)
	StatusUpdateKicked
	StatusUpdateDemoted
	StatusUpdateUnknown
)

// LobbyStatusNotify is a message from server to client notifying them of some status (e.g. the reason they were kicked).
type LobbyStatusNotify struct {
	Channel    uuid.UUID          // The channel which the status notification applies to.
	Message    []byte             // A message describing the status update. This is a maximum of 64 bytes, UTF-8 encoded.
	ExpiryTime uint64             // The time the status change takes effect until.
	Reason     StatusUpdateReason // The reason for the status notification.
}

func (m LobbyStatusNotify) String() string {
	return fmt.Sprintf("%T()", m)
}

// NewLobbyStatusNotifyv2WithArgs initializes a new LobbyStatusNotifyv2 with the provided arguments.
func NewLobbyStatusNotifyv2(channel uuid.UUID, message string, expiryTime uint64, reason StatusUpdateReason) *LobbyStatusNotify {
	return &LobbyStatusNotify{
		Channel:    channel,
		Message:    []byte(message[:64]),
		ExpiryTime: expiryTime,
		Reason:     reason,
	}
}

func (l *LobbyStatusNotify) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{

		func() error { return s.Stream(&l.Channel) },
		func() error { return s.StreamBytes(&l.Message, 64) },
		func() error { return s.Stream(&l.ExpiryTime) },
		func() error { return s.Stream(&l.Reason) },
	})
}
