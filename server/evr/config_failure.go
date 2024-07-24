package evr

import (
	"fmt"
)

type ConfigFailure struct {
	Unk0      uint64
	Unk1      uint64
	ErrorInfo ConfigErrorInfo
}

func (m ConfigFailure) String() string {
	e := m.ErrorInfo
	return fmt.Sprintf(`%T(type="%s", id="%s", code=%d, error="%s")`, m, e.Type, e.Identifier, e.ErrorCode, e.Error)
}

type ConfigErrorInfo struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
	ErrorCode  uint64 `json:"errorCode"`
	Error      string `json:"error"`
}

func (m *ConfigFailure) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Unk0) },
		func() error { return s.Stream(&m.Unk1) },
		func() error { return s.StreamJSON(&m.ErrorInfo, true, NoCompression) },
	})
}

func NewConfigFailure(typ, id string) *ConfigFailure {
	return &ConfigFailure{
		Unk0: 0,
		ErrorInfo: ConfigErrorInfo{
			Type:       typ,
			Identifier: id,
			ErrorCode:  0x00000001,
			Error:      "resource not found",
		},
	}
}
