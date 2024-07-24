package evr

import (
	"fmt"
)

const (
	DefaultErrorStatusCode = 400 // Bad Input
)

type LoginFailure struct {
	UserId       EvrID
	StatusCode   uint64 // HTTP Status code
	ErrorMessage string
}

func (m LoginFailure) String() string {
	return fmt.Sprintf("%T(user_id=%s, status_code=%d, error_message=%s)", m, m.UserId.Token(), m.StatusCode, m.ErrorMessage)
}

func (m *LoginFailure) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.UserId.PlatformCode) },
		func() error { return s.Stream(&m.UserId.AccountID) },
		func() error { return s.Stream(&m.StatusCode) },
		func() error { return s.StreamString(&m.ErrorMessage, 160) },
	})
}

func NewLoginFailure(userId EvrID, errorMessage string) *LoginFailure {
	return &LoginFailure{
		UserId:       userId,
		StatusCode:   DefaultErrorStatusCode,
		ErrorMessage: errorMessage,
	}
}
