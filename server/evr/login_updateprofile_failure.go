package evr

import (
	"fmt"
)

type UpdateProfileFailure struct {
	EvrID      EvrID
	statusCode uint64 // HTTP Status Code
	Message    string
}

func (m *UpdateProfileFailure) String() string {
	return fmt.Sprintf("%T(user_id=%s, status_code=%d, msg='%s')", m, m.EvrID.String(), m.statusCode, m.Message)
}

func (m *UpdateProfileFailure) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.EvrID) },
		func() error { return s.Stream(&m.statusCode) },
		func() error { return s.StreamNullTerminatedString(&m.Message) },
	})
}

func NewUpdateProfileFailure(evrID EvrID, statusCode uint64, message string) *UpdateProfileFailure {
	return &UpdateProfileFailure{
		EvrID:      evrID,
		statusCode: statusCode,
		Message:    message,
	}
}
