package evr

import (
	"encoding/binary"
	"fmt"
)

type UpdateProfileFailure struct {
	EvrID      EvrID
	statusCode uint64 // HTTP Status Code
	Message    string
}

func (m *UpdateProfileFailure) Token() string {
	return "SNSUpdateProfileFailure"
}

func (m *UpdateProfileFailure) Symbol() Symbol {
	return ToSymbol(m.Token())
}

func (lr *UpdateProfileFailure) String() string {
	return fmt.Sprintf("%s(user_id=%s, status_code=%d, msg='%s')", lr.Token(), lr.EvrID.String(), lr.statusCode, lr.Message)
}

func (m *UpdateProfileFailure) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.statusCode) },
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
