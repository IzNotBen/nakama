package evr

import (
	"fmt"
)

const (
	BroadcasterRegistration_InvalidRequest BroadcasterRegistrationFailureCode = iota
	BroadcasterRegistration_Timeout
	BroadcasterRegistration_CryptographyError
	BroadcasterRegistration_DatabaseError
	BroadcasterRegistration_AccountDoesNotExist
	BroadcasterRegistration_ConnectionFailure
	BroadcasterRegistration_ConnectionLost
	BroadcasterRegistration_ProviderError
	BroadcasterRegistration_Restricted
	BroadcasterRegistration_Unknown
	BroadcasterRegistration_Failure
	BroadcasterRegistration_Success
)

type BroadcasterRegistrationFailureCode byte

type BroadcasterRegistrationFailure struct {
	Code byte // The failure code for the lobby registration.
}

func NewBroadcasterRegistrationFailure(code BroadcasterRegistrationFailureCode) *BroadcasterRegistrationFailure {
	return &BroadcasterRegistrationFailure{
		Code: byte(code),
	}
}

func (m *BroadcasterRegistrationFailure) Stream(s *Stream) error {
	return s.StreamByte(&m.Code)
}

func (lr BroadcasterRegistrationFailure) String() string {
	return fmt.Sprintf("%T(result=%v)", lr, lr.Code)
}
