package evr

import (
	"fmt"
)

func (m ConfigRequest) Token() string   { return "SNSConfigRequestv2" }
func (m *ConfigRequest) Symbol() Symbol { return ToSymbol(m.Token()) }

func (m *ConfigRequest) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		//func() error { return s.StreamByte(&m.TypeTail) },
		func() error { return s.Skip(1) },
		func() error { return s.StreamJSON(&m, true, NoCompression) },
	})
}

func (m ConfigRequest) String() string {
	return fmt.Sprintf("%s(config_info=%v)", m.Token(), m.Type)
}
