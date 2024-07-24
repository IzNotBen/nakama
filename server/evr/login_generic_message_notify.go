package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type GenericMessageNotify struct {
	MessageType Symbol // ovr_social_member_data or ovr_social_member_data_nack
	Session     uuid.UUID
	RoomId      int64
	PartyData   GenericMessageData
}

func NewGenericMessageNotify(messageType Symbol, session uuid.UUID, roomId int64, partyData GenericMessageData) *GenericMessageNotify {
	return &GenericMessageNotify{
		MessageType: messageType,
		Session:     session,
		RoomId:      roomId,
		PartyData:   partyData,
	}
}

func (m *GenericMessageNotify) Stream(s *Stream) error {
	padding := make([]byte, 8)
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamBytes(&padding, 8) },
		func() error { return s.Stream(&m.RoomId) },
		func() error { return s.Stream(&m.Session) },
		func() error { return s.Stream(&m.MessageType) },
		func() error { return s.StreamJSON(&m.PartyData, true, ZstdCompression) },
	})
}

func (m *GenericMessageNotify) String() string {
	return fmt.Sprintf("%T{MessageType: %d, Session: %s, RoomId: %d, PartyData: %v}", m, m.MessageType, m.Session, m.RoomId, m.PartyData)
}

func (m *GenericMessageNotify) GetLoginSessionID() uuid.UUID {
	return m.Session
}
