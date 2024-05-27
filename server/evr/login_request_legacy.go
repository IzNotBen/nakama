package evr

import (
	"encoding/binary"
	"fmt"
)

// LoginRequestV1 represents a message from client to server requesting for a user sign-in.
type LoginRequestV1 struct {
	Session GUID // This is the old session id, if it had one.
	EvrID   EvrId
	Unk1    [3]string
	Unk2    [5]byte
	Profile LoginProfileV1
}

func (lr LoginRequestV1) String() string {
	return fmt.Sprintf("LoginRequestV1(session=%v, user_id=%s)",
		lr.Session, lr.EvrID.String())
}

func (m LoginRequestV1) GetEvrID() EvrId {
	return m.EvrID
}

func (m LoginRequestV1) GetSessionID() GUID {
	return m.Session
}

func (m LoginRequestV1) GetLoginProfile() LoginProfileV1 {
	return m.Profile
}

func (m *LoginRequestV1) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGUID(&m.Session) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.AccountId) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk1) },
		func() error {
			return s.StreamJSON(&m.Profile, true, NoCompression)
		},
	})
}

func NewLoginRequestV1(session GUID, userId EvrId, loginData LoginProfileV1) (*LoginRequestV1, error) {
	return &LoginRequestV1{
		Session: session,
		EvrID:   userId,
		Profile: loginData,
	}, nil
}

type LoginProfileV1 struct {
	AccessToken      string             `json:"access_token"`
	Accountid        int64              `json:"accountid"`
	AppID            uint64             `json:"appid"`
	GameSettings     GameSettingsV1     `json:"game_settings"`
	GraphicsSettings GraphicsSettingsV1 `json:"graphics_settings"`
	HMDSerialNumber  string             `json:"hmdserialnumber"`
	Lobbyversion     uint64             `json:"lobbyversion"`
	Nonce            string             `json:"nonce"`
	PublisherLock    string             `json:"publisher_lock"`
	SystemInfo       SystemInfoV1       `json:"system_info"`
}

func (p LoginProfileV1) GetHeadsetType() string {
	return p.SystemInfo.HeadsetType
}

func (p LoginProfileV1) GetAppID() uint64 {
	return p.AppID
}

func (p LoginProfileV1) GetHMDSerialNumber() string {
	return p.HMDSerialNumber
}

func (p LoginProfileV1) GetLobbyVersion() uint64 {
	return p.Lobbyversion
}

func (p LoginProfileV1) GetPublisherLock() string {
	return p.PublisherLock
}

type GameSettingsV1 struct {
	EnablePitch          bool    `json:"EnablePitch"`
	EnableRoll           bool    `json:"EnableRoll"`
	EnableSmoothRotation bool    `json:"EnableSmoothRotation"`
	EnableYaw            bool    `json:"EnableYaw"`
	Hud                  bool    `json:"HUD"`
	HighPerformanceMode  bool    `json:"HighPerformanceMode"`
	VOIPAlwaysOn         bool    `json:"VOIPAlwaysOn"`
	VOIPPushToMute       bool    `json:"VOIPPushToMute"`
	VOIPPushToTalk       bool    `json:"VOIPPushToTalk"`
	Vibration            bool    `json:"Vibration"`
	Announcer            float32 `json:"announcer"`
	Music                float32 `json:"music"`
	Sfx                  float32 `json:"sfx"`
	Smoothrotationspeed  float32 `json:"smoothrotationspeed"`
	Voip                 float32 `json:"voip"`
}
type GraphicsSettingsV1 struct {
	// WARNING: EchoVR dictates this schema.
	TemporalAA                        float64   `json:"temporalaa"`
	Fullscreen                        float32   `json:"fullscreen"`
	Display                           int64     `json:"display"`
	ResolutionScale                   float32   `json:"resolutionscale"`
	AdaptiveResolutionTargetFramerate int64     `json:"adaptiverestargetframerate"`
	AdaptiveResolutionMaxScale        float32   `json:"adaptiveresmaxscale"`
	AdaptiveResolution                float32   `json:"adaptiveresolution"`
	AdaptiveResolutionMinScale        float32   `json:"adaptiveresminscale"`
	AdaptiveResolutionHeadroom        float32   `json:"adaptiveresheadroom"`
	QualityLevel                      int64     `json:"qualitylevel"`
	Quality                           QualityV1 `json:"quality"`
	MSAA                              int64     `json:"msaa"`
	Sharpening                        float32   `json:"sharpening"`
	MultiResolution                   float32   `json:"multires"`
	Gamma                             float32   `json:"gamma"`
	CaptureFOV                        float32   `json:"capturefov"`
}
type QualityV1 struct {
	// WARNING: EchoVR dictates this schema.
	ShadowResolution   int64   `json:"shadowresolution"`
	FX                 int64   `json:"fx"`
	Bloom              float32 `json:"bloom"`
	CascadeResolution  int64   `json:"cascaderesolution"`
	CascadeDistance    float32 `json:"cascadedistance"`
	Textures           int64   `json:"textures"`
	ShadowMSAA         int64   `json:"shadowmsaa"`
	Meshes             int64   `json:"meshes"`
	ShadowFilterScale  float32 `json:"shadowfilterscale"`
	StaggerFarCascades float32 `json:"staggerfarcascades"`
	Volumetrics        float32 `json:"volumetrics"`
	Lights             int64   `json:"lights"`
	Shadows            int64   `json:"shadows"`
	Anims              int64   `json:"anims"`
}
type SystemInfoV1 struct {
	CPU                string `json:"cpu"`
	DedicatedGPUMemory int64  `json:"dedicated_gpu_memory"`
	DriverVersion      string `json:"driver_version"`
	HeadsetType        string `json:"headset_type"`
	MemoryTotal        int64  `json:"memory_total"`
	MemoryUsed         int64  `json:"memory_used"`
	NetworkType        string `json:"network_type"`
	NumLogicalCores    int64  `json:"num_logical_cores"`
	NumPhysicalCores   int64  `json:"num_physical_cores"`
	VideoCard          string `json:"video_card"`
}
