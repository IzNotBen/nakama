package evr

import (
	"encoding/binary"
)

type UserServerProfileUpdateRequest struct {
	EvrID   EvrId
	Payload UpdatePayload
}

func (m UserServerProfileUpdateRequest) Token() string {
	return "SNSUserServerProfileUpdateRequest"
}

func (m *UserServerProfileUpdateRequest) Symbol() Symbol {
	return SymbolOf(m)
}

func (m UserServerProfileUpdateRequest) String() string {
	return m.Token()
}

func (m *UserServerProfileUpdateRequest) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID) },
		func() error { return s.StreamJSON(&m.Payload, true, NoCompression) },
	})
}

type UpdatePayload struct {
	Matchtype Symbol      `json:"matchtype"`
	SessionID GUID        `json:"sessionid"`
	Update    StatsUpdate `json:"update"`
}

/*
	"ArenaLosses": {
			"op": "add",
			"val": 1
		},
*/
type StatsUpdate struct {
	StatsGroups map[string]map[string]any `json:"stats"`
}
