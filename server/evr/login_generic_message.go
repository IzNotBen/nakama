package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type GenericMessage struct {
	Session     uuid.UUID
	AcctId      uint64
	MessageType Symbol
	OtherEvrID  EvrID
	RoomID      int64
	PartyData   GenericMessageData
}

func NewGenericMessage(session uuid.UUID, acctId uint64, messageType Symbol, otherEvrID EvrID, partyData GenericMessageData) *GenericMessage {
	return &GenericMessage{
		Session:     session,
		AcctId:      acctId,
		MessageType: messageType,
		OtherEvrID:  otherEvrID,
		PartyData:   partyData,
	}
}

func (m *GenericMessage) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Session) },
		func() error { return s.Stream(&m.AcctId) },
		func() error { return s.Stream(&m.MessageType) },
		func() error { return s.Stream(&m.OtherEvrID) },
		func() error {
			// ovr_social_member_data_nack
			if m.MessageType == 0xb9ea35ff8448e615 {
				return s.Stream(&m.RoomID)
			} else {
				return s.StreamJSON(&m.PartyData, true, ZstdCompression)
			}
		},
	})
}

func (m *GenericMessage) GetLoginSessionID() uuid.UUID {
	return m.Session
}

func (m GenericMessage) String() string {
	return fmt.Sprintf("%T{Session: %s, AcctId: %d, OVRSymbol: %d, OtherEvrID: %s, RoomId: %d, PartyData: %v}", m, m.Session, m.AcctId, m.MessageType, m.OtherEvrID.String(), m.RoomID, m.PartyData)
}

type GenericMessageData struct {
	Mode        int64  `json:"matchtype"`
	HeadsetType int    `json:"headsettype"`
	Status      string `json:"status"`
	LobbyType   uint8  `json:"lobbytype"`
	LobbyId     string `json:"lobbyid"`
	Team        int16  `json:"team"`
	RoomId      uint64 `json:"roomid"`
}

func NewGenericMessageData(matchType Symbol, headsetType int, status string, lobbyType LobbyType, lobbyId string, team int, roomId int) *GenericMessageData {
	return &GenericMessageData{
		Mode:        int64(matchType),
		HeadsetType: int(headsetType),
		Status:      status,
		LobbyType:   uint8(lobbyType),
		LobbyId:     lobbyId,
		Team:        int16(team),
		RoomId:      uint64(roomId),
	}
}
