package evr

import (
	"fmt"
	"net/http"
)

// nakama -> client: failure response to LoggedInUserProfileFailure.
type LoggedInUserProfileFailure struct {
	EvrID        EvrID
	StatusCode   uint64 // HTTP status code
	ErrorMessage string
}

func (m *LoggedInUserProfileFailure) String() string {
	return fmt.Sprintf("%T(user_id=%v, status=%v, msg=\"%s\")", m, m.EvrID, http.StatusText(int(m.StatusCode)), m.ErrorMessage)
}

func (m *LoggedInUserProfileFailure) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.EvrID.PlatformCode) },
		func() error { return s.Stream(&m.EvrID.AccountID) },
		func() error { return s.Stream(&m.StatusCode) },
		func() error { return s.StreamNullTerminatedString(&m.ErrorMessage) },
	})
}

func NewLoggedInUserProfileFailure(evrID EvrID, statusCode int, message string) *LoggedInUserProfileFailure {
	return &LoggedInUserProfileFailure{
		EvrID:        evrID,
		StatusCode:   uint64(statusCode),
		ErrorMessage: message,
	}
}
