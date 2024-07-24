package evr

import (
	"fmt"
	"net/http"
)

// OtherUserProfileFailure represents a message from server to client indicating a failure in OtherUserProfileRequest.
type OtherUserProfileFailure struct {
	EvrID      EvrID  // The identifier of the associated user.
	StatusCode uint64 // The status code returned with the failure. (These are http status codes)
	Message    string // The message returned with the failure.
}

func NewOtherUserProfileFailure(evrID EvrID, statusCode uint64, message string) *OtherUserProfileFailure {
	return &OtherUserProfileFailure{
		EvrID:      evrID,
		StatusCode: statusCode,
		Message:    message,
	}
}

func (m *OtherUserProfileFailure) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.EvrID) },
		func() error { return s.Stream(&m.StatusCode) },
		func() error { return s.StreamNullTerminatedString(&m.Message) },
	})
}

func (m *OtherUserProfileFailure) String() string {
	return fmt.Sprintf("%T(user_id=%v, status=%v, msg=\"%s\")", m, m.EvrID, http.StatusText(int(m.StatusCode)), m.Message)
}
