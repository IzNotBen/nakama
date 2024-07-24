package evr

import (
	"encoding/json"
	"fmt"
)

// FindServerRegionInfo is a message from server to the client providing information on servers available in different regions.
// This is not necessary for a client to operate.
type FindServerRegionInfo struct {
	Unk0       uint16
	Unk1       uint16
	Unk2       uint16
	RegionInfo map[string]interface{}
}

// NewFindServerRegionInfoWithArgs initializes a new FindServerRegionInfo message with the provided arguments.
func NewFindServerRegionInfo(unk0, unk1, unk2 uint16, regionInfo map[string]interface{}) *FindServerRegionInfo {
	return &FindServerRegionInfo{
		Unk0:       unk0,
		Unk1:       unk1,
		Unk2:       unk2,
		RegionInfo: regionInfo,
	}
}

func (m *FindServerRegionInfo) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Unk0) },
		func() error { return s.Stream(&m.Unk1) },
		func() error { return s.Stream(&m.Unk2) },
		func() error { return s.StreamJSON(&m.RegionInfo, false, NoCompression) },
	})
}

func (m *FindServerRegionInfo) String() string {
	regionJson, err := json.Marshal(m.RegionInfo)
	if err != nil {
		regionJson = []byte(fmt.Sprintf("error: %s", err))
	}

	return fmt.Sprintf("%T(unk0=%d, unk1=%d, unk2=%d, region_info=%s)", m, m.Unk0, m.Unk1, m.Unk2, regionJson)
}
