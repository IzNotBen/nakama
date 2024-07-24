package evr

import (
	"encoding/binary"
	"fmt"
	"net/http"
)

// SNSLobbySmiteEntrant represents a message from server to client indicating a failure in OtherUserProfileRequest.
type SNSLobbySmiteEntrant struct {
	EvrID      EvrID  // The identifier of the associated user.
	StatusCode uint64 // The status code returned with the failure. (These are http status codes)
	Message    string // The message returned with the failure.
}

func (m *SNSLobbySmiteEntrant) Token() string {
	return "SNSSNSLobbySmiteEntrant"
}

func (m *SNSLobbySmiteEntrant) Symbol() Symbol {
	return SymbolOf(m)
}

func NewSNSLobbySmiteEntrant(evrID EvrID, statusCode uint64, message string) *SNSLobbySmiteEntrant {
	return &SNSLobbySmiteEntrant{
		EvrID:      evrID,
		StatusCode: statusCode,
		Message:    message,
	}
}

func (m *SNSLobbySmiteEntrant) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamStruct(&m.EvrID) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.StatusCode) },
		func() error { return s.StreamNullTerminatedString(&m.Message) },
	})
}

func (m *SNSLobbySmiteEntrant) String() string {
	return fmt.Sprintf("%s(user_id=%v, status=%v, msg=\"%s\")", m.Token(), m.EvrID, http.StatusText(int(m.StatusCode)), m.Message)
}
