package evr

import (
	"fmt"
)

// BroadcasterSessionStateChange is a message from game server to server, indicating the game server's session StateChange.
// NOTE: This is an unofficial message created for Echo Relay.

type BroadcasterSessionState struct {
	SessionID GUID
}

type BroadcasterSessionStateV1 BroadcasterSessionState
type BroadcasterSessionStateV2 BroadcasterSessionState

type BroadcasterSessionStartedV1 struct {
	BroadcasterSessionStateV1
}
type BroadcasterSessionStartedV2 struct {
	BroadcasterSessionStateV2
}

type BroadcasterSessionEndedV1 struct {
	BroadcasterSessionStateV1
}
type BroadcasterSessionEndedV2 struct {
	BroadcasterSessionStateV2
}

type BroadcasterSessionLockedV1 struct {
	BroadcasterSessionStateV1
}
type BroadcasterSessionLockedV2 struct {
	BroadcasterSessionStateV2
}

type BroadcasterSessionUnlockedV1 struct {
	BroadcasterSessionStateV1
}
type BroadcasterSessionUnlockedV2 struct {
	BroadcasterSessionStateV2
}

type BroadcasterSessionErrored struct {
	BroadcasterSessionStateV2
}

func (m BroadcasterSessionState) Version1() *BroadcasterSessionStateV1 {
	return &BroadcasterSessionStateV1{
		SessionID: m.SessionID,
	}
}

func (m BroadcasterSessionState) Version2() *BroadcasterSessionStateV2 {
	return &BroadcasterSessionStateV2{
		SessionID: m.SessionID,
	}
}

func (m *BroadcasterSessionStateV1) Stream(s *Stream) error {
	return s.Skip(1)
}

func (m *BroadcasterSessionStateV2) Stream(s *Stream) error {
	return s.Stream(&m.SessionID)
}

func (m BroadcasterSessionState) String() string {
	return fmt.Sprintf("BroadcasterSessionStateChange(session_id=%s)", m.SessionID.String())
}
