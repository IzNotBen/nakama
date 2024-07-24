package evr

import (
	"fmt"
)

type ChannelInfoRequest struct {
}

func (m *ChannelInfoRequest) Stream(s *Stream) error {
	return s.Skip(1)
}

func (m ChannelInfoRequest) String() string {
	return fmt.Sprintf("%T()", m)
}
