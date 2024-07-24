package evr

import (
	"fmt"
)

type UpdateProfileSuccess struct {
	UserId EvrID
}

func (m *UpdateProfileSuccess) String() string {
	return fmt.Sprintf("%T(user_id=%s)", m, m.UserId.String())
}

func (m *UpdateProfileSuccess) Stream(s *Stream) error {
	return s.Stream(&m.UserId)
}
func NewSNSUpdateProfileSuccess(userId *EvrID) *UpdateProfileSuccess {
	return &UpdateProfileSuccess{
		UserId: *userId,
	}
}
