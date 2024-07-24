package evr

import (
	"fmt"
)

var SNSUserServerProfileUpdateSuccessSymbol Symbol = ToSymbol("SNSUserServerProfileUpdateSuccess")

type UserServerProfileUpdateSuccess struct {
	EvrID EvrID
}

func (m *UserServerProfileUpdateSuccess) Token() string {
	return "SNSUserServerProfileUpdateSuccess"
}

func (m *UserServerProfileUpdateSuccess) Symbol() Symbol {
	return SymbolOf(m)
}

func (lr *UserServerProfileUpdateSuccess) String() string {
	return fmt.Sprintf("%s(user_id=%s)", lr.Token(), lr.EvrID.String())
}
func (m *UserServerProfileUpdateSuccess) Stream(s *Stream) error {
	return s.StreamStruct(&m.EvrID)
}
func NewUserServerProfileUpdateSuccess(userId EvrID) *UserServerProfileUpdateSuccess {
	return &UserServerProfileUpdateSuccess{
		EvrID: userId,
	}
}
