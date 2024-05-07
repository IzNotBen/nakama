package evr

import (
	"encoding/binary"
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type LoginRequest interface {
	GetSessionID() uuid.UUID
	GetEvrID() EvrId
	GetLoginProfile() LoginProfile
}

var _ = LoginRequest(&LoginRequestV2{})

// LoginRequestV2 represents a message from client to server requesting for a user sign-in.
type LoginRequestV2 struct {
	Session uuid.UUID    `json:"Session"` // This is the old session id, if it had one.
	EvrId   EvrId        `json:"UserId"`
	Profile LoginProfile `json:"LoginData"`
}

func (lr LoginRequestV2) GetEvrID() EvrId {
	return lr.EvrId
}

func (m LoginRequestV2) GetSessionID() uuid.UUID {
	return m.Session
}

func (m LoginRequestV2) GetLoginProfile() LoginProfile {
	return m.Profile
}

func (lr LoginRequestV2) String() string {
	return fmt.Sprintf("LoginRequestV2(session=%v, user_id=%s, login_data=%s)", lr.Session, lr.EvrId.String(), lr.Profile.String())
}

func (m *LoginRequestV2) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(&m.Session) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrId.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrId.AccountId) },
		func() error { return s.StreamJson(&m.Profile, true, NoCompression) },
	})
}

func NewLoginRequest(session uuid.UUID, userId EvrId, loginData LoginProfile) (*LoginRequestV2, error) {
	return &LoginRequestV2{
		Session: session,
		EvrId:   userId,
		Profile: loginData,
	}, nil
}

type LoginProfile struct {
	// WARNING: EchoVR dictates this schema.
	AccountId                   uint64     `json:"accountid"`
	DisplayName                 string     `json:"displayname"`
	BypassAuth                  bool       `json:"bypassauth"`
	AccessToken                 string     `json:"access_token"`
	Nonce                       string     `json:"nonce"`
	BuildVersion                int64      `json:"buildversion"`
	LobbyVersion                uint64     `json:"lobbyversion"`
	AppId                       uint64     `json:"appid"`
	PublisherLock               string     `json:"publisher_lock"`
	HmdSerialNumber             string     `json:"hmdserialnumber"`
	DesiredClientProfileVersion int64      `json:"desiredclientprofileversion"`
	SystemInfo                  SystemInfo `json:"system_info"`
}

func (ld *LoginProfile) String() string {
	return fmt.Sprintf("%s(account_id=%d, display_name=%s, hmd_serial_number=%s, "+
		")", "LoginData", ld.AccountId, ld.DisplayName, ld.HmdSerialNumber)
}

func (p LoginProfile) GetHeadsetType() string {
	return p.SystemInfo.HeadsetType
}

func (p LoginProfile) GetPublisherLock() string {
	return p.PublisherLock
}

func (p LoginProfile) GetLobbyVersion() uint64 {
	return p.LobbyVersion
}

func (p LoginProfile) GetAppID() uint64 {
	return p.AppId
}

func (p LoginProfile) GetHMDSerialNumber() string {
	return p.HmdSerialNumber
}

type GraphicsSettings struct {
	// WARNING: EchoVR dictates this schema.
	TemporalAA                        bool    `json:"temporalaa"`
	Fullscreen                        bool    `json:"fullscreen"`
	Display                           int64   `json:"display"`
	ResolutionScale                   float32 `json:"resolutionscale"`
	AdaptiveResolutionTargetFramerate int64   `json:"adaptiverestargetframerate"`
	AdaptiveResolutionMaxScale        float32 `json:"adaptiveresmaxscale"`
	AdaptiveResolution                bool    `json:"adaptiveresolution"`
	AdaptiveResolutionMinScale        float32 `json:"adaptiveresminscale"`
	AdaptiveResolutionHeadroom        float32 `json:"adaptiveresheadroom"`
	QualityLevel                      int64   `json:"qualitylevel"`
	Quality                           Quality `json:"quality"`
	MSAA                              int64   `json:"msaa"`
	Sharpening                        float32 `json:"sharpening"`
	MultiResolution                   bool    `json:"multires"`
	Gamma                             float32 `json:"gamma"`
	CaptureFOV                        float32 `json:"capturefov"`
}

type Quality struct {
	// WARNING: EchoVR dictates this schema.
	ShadowResolution   int64   `json:"shadowresolution"`
	FX                 int64   `json:"fx"`
	Bloom              bool    `json:"bloom"`
	CascadeResolution  int64   `json:"cascaderesolution"`
	CascadeDistance    float32 `json:"cascadedistance"`
	Textures           int64   `json:"textures"`
	ShadowMSAA         int64   `json:"shadowmsaa"`
	Meshes             int64   `json:"meshes"`
	ShadowFilterScale  float32 `json:"shadowfilterscale"`
	StaggerFarCascades bool    `json:"staggerfarcascades"`
	Volumetrics        bool    `json:"volumetrics"`
	Lights             int64   `json:"lights"`
	Shadows            int64   `json:"shadows"`
	Anims              int64   `json:"anims"`
}

type SystemInfo struct {
	// WARNING: EchoVR dictates this schema.
	HeadsetType        string `json:"headset_type"`
	DriverVersion      string `json:"driver_version"`
	NetworkType        string `json:"network_type"`
	VideoCard          string `json:"video_card"`
	CPU                string `json:"cpu"`
	NumPhysicalCores   int64  `json:"num_physical_cores"`
	NumLogicalCores    int64  `json:"num_logical_cores"`
	MemoryTotal        int64  `json:"memory_total"`
	MemoryUsed         int64  `json:"memory_used"`
	DedicatedGPUMemory int64  `json:"dedicated_gpu_memory"`
}
