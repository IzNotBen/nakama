package evr

import (
	"fmt"
)

var SNSUserServerProfileUpdateSuccessSymbol Symbol = ToSymbol("SNSUserServerProfileUpdateSuccess")

type UserServerProfileUpdateSuccess struct {
	EvrID EvrID
}

func (m *UserServerProfileUpdateSuccess) String() string {
	return fmt.Sprintf("%T(user_id=%s)", m, m.EvrID.String())
}
func (m *UserServerProfileUpdateSuccess) Stream(s *Stream) error {
	return s.Stream(&m.EvrID)
}
func NewUserServerProfileUpdateSuccess(userId EvrID) *UserServerProfileUpdateSuccess {
	return &UserServerProfileUpdateSuccess{
		EvrID: userId,
	}
}
