package evr

type LobbySessionRequest interface {
	GetChannel() GUID
	GetMode() Symbol
}
