package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type LobbySessionErrorCode uint32

const (
	LobbySessionFailure_Timeout0 LobbySessionErrorCode = iota
	LobbySessionFailure_UpdateRequired
	LobbySessionFailure_BadRequest
	LobbySessionFailure_Timeout3
	LobbySessionFailure_ServerDoesNotExist
	LobbySessionFailure_ServerIsIncompatible
	LobbySessionFailure_ServerFindFailed
	LobbySessionFailure_ServerIsLocked
	LobbySessionFailure_ServerIsFull
	LobbySessionFailure_InternalError
	LobbySessionFailure_MissingEntitlement
	LobbySessionFailure_BannedFromLobbyGroup
	LobbySessionFailure_KickedFromLobbyGroup
	LobbySessionFailure_NotALobbyGroupMod
)

type LobbySessionFailureCodes struct {
	Timeout0             LobbySessionErrorCode
	UpdateRequired       LobbySessionErrorCode
	BadRequest           LobbySessionErrorCode
	Timeout3             LobbySessionErrorCode
	ServerDoesNotExist   LobbySessionErrorCode
	ServerIsIncompatible LobbySessionErrorCode
	ServerFindFailed     LobbySessionErrorCode
	ServerIsLocked       LobbySessionErrorCode
	ServerIsFull         LobbySessionErrorCode
	InternalError        LobbySessionErrorCode
	MissingEntitlement   LobbySessionErrorCode
	BannedFromLobbyGroup LobbySessionErrorCode
	KickedFromLobbyGroup LobbySessionErrorCode
	NotALobbyGroupMod    LobbySessionErrorCode
}

var LobbySessionFailures = LobbySessionFailureCodes{
	Timeout0:             0,
	UpdateRequired:       1,
	BadRequest:           2,
	Timeout3:             3,
	ServerDoesNotExist:   4,
	ServerIsIncompatible: 5,
	ServerFindFailed:     6,
	ServerIsLocked:       7,
	ServerIsFull:         8,
	InternalError:        9,
	MissingEntitlement:   10,
	BannedFromLobbyGroup: 11,
	KickedFromLobbyGroup: 12,
	NotALobbyGroupMod:    13,
}

// LobbySessionFailure is a message from server to client indicating a lobby session request failed.
type LobbySessionFailure struct {
	Mode    Symbol                // A symbol representing the gametype requested for the session.
	GroupID uuid.UUID             // The channel requested for the session.
	Code    LobbySessionErrorCode // The error code to return with the failure.
	Unk0    uint32                // TODO: Add description
	Message string                // The message sent with the failure.
}

func NewLobbySessionFailure(gameType Symbol, channel uuid.UUID, errorCode LobbySessionErrorCode, message string) *LobbySessionFailure {
	return &LobbySessionFailure{
		Mode:    gameType,
		GroupID: channel,
		Code:    errorCode,
		Unk0:    255,
		Message: message,
	}
}

func (m LobbySessionFailure) String() string {
	return fmt.Sprintf("%T(game_type=%d, channel=%s, error_code=%d, unk0=%d, msg=%s)", m, m.Mode, m.GroupID.String(), m.Code, m.Unk0, m.Message)
}

func (m LobbySessionFailure) Version1() *LobbySessionFailurev1 {
	return &LobbySessionFailurev1{
		Code: LobbySessionErrorCode(m.Code),
	}
}
func (m LobbySessionFailure) Version2() *LobbySessionFailurev2 {
	v2 := LobbySessionFailurev2(m)
	return &v2
}

func (m LobbySessionFailure) Version3() *LobbySessionFailurev3 {
	return &LobbySessionFailurev3{

		Mode:    m.Mode,
		GroupID: m.GroupID,
		Code:    m.Code,
		Unk0:    m.Unk0,
	}
}

func (m LobbySessionFailure) Version4() *LobbySessionFailurev4 {
	v4 := LobbySessionFailurev4(m)
	return &v4
}

type LobbySessionFailurev1 LobbySessionFailure

func (m LobbySessionFailurev1) String() string {
	return fmt.Sprintf("%T(error_code=%d)", m, m.Code)
}

func (m *LobbySessionFailurev1) Stream(s *Stream) error {
	return s.Stream(&m.Code)
}

type LobbySessionFailurev2 LobbySessionFailure

func (m LobbySessionFailurev2) String() string {
	return fmt.Sprintf("%T(channel_uuid=%s, error_code=%d)", m, m.GroupID.String(), m.Code)
}

func (m *LobbySessionFailurev2) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.GroupID) },
		func() error { return s.Stream(&m.Code) },
	})
}

type LobbySessionFailurev3 LobbySessionFailure

func (m *LobbySessionFailurev3) String() string {
	return fmt.Sprintf("%T(game_type=%d, channel=%s, error_code=%d)", m, m.Mode, m.GroupID.String(), m.Code)
}

func (m *LobbySessionFailurev3) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Mode) },
		func() error { return s.Stream(&m.GroupID) },
		func() error { return s.Stream(&m.Code) },
		func() error { return s.Stream(&m.Unk0) },
	})
}

type LobbySessionFailurev4 LobbySessionFailure

func (m LobbySessionFailurev4) String() string {
	return fmt.Sprintf("LobbySessionFailurev4(game_type=%d, channel=%s, error_code=%d, msg=%s)", m.Mode, m.GroupID.String(), m.Code, m.Message)
}

func (m *LobbySessionFailurev4) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Mode) },
		func() error { return s.Stream(&m.GroupID) },
		func() error { return s.Stream(&m.Code) },
		func() error { return s.Stream(&m.Unk0) },
		func() error { return s.StreamString(&m.Message, 72) },
	})
}
