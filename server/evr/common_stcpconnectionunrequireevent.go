package evr

type STcpConnectionUnrequireEvent struct {
	_ byte
}

func (m *STcpConnectionUnrequireEvent) Stream(s *Stream) error {
	return s.Skip(1)
}

func (m STcpConnectionUnrequireEvent) String() string {
	return "STcpConnectionUnrequireEvent"
}

func NewSTcpConnectionUnrequireEvent() *STcpConnectionUnrequireEvent {
	return &STcpConnectionUnrequireEvent{}
}
