package evr

import (
	"testing"
)

func TestPacketEncoderSettingsFromFlags(t *testing.T) {
	tests := []struct {
		flags              uint64
		expectedEncryption bool
		expectedMac        bool
		expectedDigestSize int
		expectedIteration  int
		expectedMacKeySize int
		expectedKeySize    int
		expectedRandomSize int
	}{
		{0, false, false, 0, 0, 0, 0, 0},                               // test with all flags disabled
		{1, true, false, 0, 0, 0, 0, 0},                                // test with encryption flag enabled
		{2, false, true, 0, 0, 0, 0, 0},                                // test with mac flag enabled
		{3, true, true, 0, 0, 0, 0, 0},                                 // test with both encryption and mac flags enabled
		{36037595259470083, true, true, 0x40, 0x00, 0x20, 0x20, 0x20},  // default client flags
		{36037595259469955, true, true, 0x20, 0x00, 0x20, 0x20, 0x20},  // default server flags
		{0x0080080080000103, true, true, 0x40, 0x00, 0x20, 0x20, 0x20}, // default client flags
		{0x0080080080000083, true, true, 0x20, 0x00, 0x20, 0x20, 0x20}, // default server flags
	}

	for _, tt := range tests {
		settings := PacketEncoderSettingsFromFlags(tt.flags)
		if settings.ToFlags() != tt.flags {
			t.Errorf("ToFlags() = %v, want %v", settings.ToFlags(), tt.flags)
		}
		if settings.EncryptionEnabled != tt.expectedEncryption {
			t.Errorf("EncryptionEnabled = %v, want %v", settings.EncryptionEnabled, tt.expectedEncryption)
		}
		if settings.MacEnabled != tt.expectedMac {
			t.Errorf("MacEnabled = %v, want %v", settings.MacEnabled, tt.expectedMac)
		}
		if settings.MacDigestSize != tt.expectedDigestSize {
			t.Errorf("MacDigestSize = %v, want %v", settings.MacDigestSize, tt.expectedDigestSize)
		}
		if settings.MacPBKDF2IterationCount != tt.expectedIteration {
			t.Errorf("MacPBKDF2IterationCount = %v, want %v", settings.MacPBKDF2IterationCount, tt.expectedIteration)
		}
		if settings.MacKeySize != tt.expectedMacKeySize {
			t.Errorf("MacKeySize = %v, want %v", settings.MacKeySize, tt.expectedMacKeySize)
		}
		if settings.EncryptionKeySize != tt.expectedKeySize {
			t.Errorf("EncryptionKeySize = %v, want %v", settings.EncryptionKeySize, tt.expectedKeySize)
		}
		if settings.RandomKeySize != tt.expectedRandomSize {
			t.Errorf("RandomKeySize = %v, want %v", settings.RandomKeySize, tt.expectedRandomSize)
		}
	}
}

func TestPacketEncoderSettingsToFlags(t *testing.T) {
	tests := []struct {
		settings *PacketEncoderSettings
		flags    uint64
	}{
		{&PacketEncoderSettings{EncryptionEnabled: false, MacEnabled: false, MacDigestSize: 0, MacPBKDF2IterationCount: 0, MacKeySize: 0, EncryptionKeySize: 0, RandomKeySize: 0}, 0},
		{&PacketEncoderSettings{EncryptionEnabled: true, MacEnabled: false, MacDigestSize: 0, MacPBKDF2IterationCount: 0, MacKeySize: 0, EncryptionKeySize: 0, RandomKeySize: 0}, 1},
		{&PacketEncoderSettings{EncryptionEnabled: false, MacEnabled: true, MacDigestSize: 0, MacPBKDF2IterationCount: 0, MacKeySize: 0, EncryptionKeySize: 0, RandomKeySize: 0}, 2},
		{&PacketEncoderSettings{EncryptionEnabled: true, MacEnabled: true, MacDigestSize: 0, MacPBKDF2IterationCount: 0, MacKeySize: 0, EncryptionKeySize: 0, RandomKeySize: 0}, 3},
		{&PacketEncoderSettings{EncryptionEnabled: true, MacEnabled: true, MacDigestSize: 0x40, MacPBKDF2IterationCount: 0x00, MacKeySize: 0x20, EncryptionKeySize: 0x20, RandomKeySize: 0x20}, 36037595259470083},
		{&PacketEncoderSettings{EncryptionEnabled: true, MacEnabled: true, MacDigestSize: 0x20, MacPBKDF2IterationCount: 0x00, MacKeySize: 0x20, EncryptionKeySize: 0x20, RandomKeySize: 0x20}, 36037595259469955},
	}

	for _, tt := range tests {
		if tt.settings.ToFlags() != tt.flags {
			t.Errorf("ToFlags() = %v, want %v", tt.settings.ToFlags(), tt.flags)
		}
	}
}
