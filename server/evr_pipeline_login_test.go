package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

func TestParseDeviceId(t *testing.T) {
	type args struct {
		token string
	}
	tests := []struct {
		name    string
		args    args
		want    *DeviceAuth
		wantErr bool
	}{
		{
			"valid token",
			args{
				token: "1343218412343402:OVR_ORG-3961234097123078:N/A:127.0.0.1",
			},
			&DeviceAuth{
				AppID: 1343218412343402,
				EvrID: evr.EvrID{
					PlatformCode: 4,
					AccountID:    3961234097123078,
				},
				HMDSerialNumber: "N/A",
				ClientIP:        "127.0.0.1",
			},
			false,
		},
		{
			"valid token, empty headset ID",
			args{
				token: "0:DMO-463990143344164620::104.8.177.198",
			},
			&DeviceAuth{
				AppID: 0,
				EvrID: evr.EvrID{
					PlatformCode: 3,
					AccountID:    463990143344164620,
				},
				HMDSerialNumber: "",
				ClientIP:        "104.8.177.198",
			},
			false,
		},
		{
			"empty string",
			args{
				token: "",
			},
			nil,
			true,
		},
		{
			"empty fields",
			args{
				token: "::",
			},
			nil,
			true,
		},
		{
			"symbols at end",
			args{
				token: "1343218412343402:OVR_ORG-3961234097123078:!@#!$##::@1203:\n!!!",
			},
			&DeviceAuth{
				AppID: 1343218412343402,
				EvrID: evr.EvrID{
					PlatformCode: 4,
					AccountID:    3961234097123078,
				},
				HMDSerialNumber: "!@#!$##::@1203:\n!!!",
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeviceAuthToken(tt.args.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDeviceId() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseDeviceId() = %v, want %v", got, tt.want)
			}
		})
	}
}

type MockEvrPipeline struct {
	EvrPipeline
}

var _ = DiscordRegistry(testDiscordRegistry{})

type testDiscordRegistry struct {
	DiscordRegistry
}

func Test_updateProfileStats(t *testing.T) {
	type args struct {
		logger  *zap.Logger
		profile *GameProfileData
		update  evr.UpdatePayload
	}

	jsonData := `{
		"matchtype": -3791849610740453400,
		"sessionid": "37A5F6FF-FA07-4CEE-9D9B-61D3F0663070",
		"update": {
			"stats": {
				"arena": {
					"ArenaLosses": {
						"op": "add",
						"val": 1
					},
					"AveragePossessionTimePerGame": {
						"op": "rep",
						"val": 8.9664583
					},
					"AverageTopSpeedPerGame": {
						"op": "rep",
						"val": 47.514111
					},
					"Catches": {
						"op": "add",
						"val": 1
					},
					"Clears": {
						"op": "add",
						"val": 1
					},
					"HighestStuns": {
						"op": "max",
						"val": 3
					},
					"Level": {
						"op": "add",
						"val": 2
					},
					"Passes": {
						"op": "add",
						"val": 1
					},
					"PossessionTime": {
						"op": "add",
						"val": 8.9664583
					},
					"PunchesReceived": {
						"op": "add",
						"val": 6
					},
					"ShotsOnGoalAgainst": {
						"op": "add",
						"val": 18
					},
					"Stuns": {
						"op": "add",
						"val": 3
					},
					"StunsPerGame": {
						"op": "rep",
						"val": 3
					},
					"TopSpeedsTotal": {
						"op": "add",
						"val": 47.514111
					},
					"XP": {
						"op": "add",
						"val": 500
					}
				},
				"daily_2024_04_12": {
					"ArenaLosses": {
						"op": "add",
						"val": 1
					},
					"AveragePossessionTimePerGame": {
						"op": "rep",
						"val": 8.9664583
					},
					"AverageTopSpeedPerGame": {
						"op": "rep",
						"val": 47.514111
					},
					"Catches": {
						"op": "add",
						"val": 1
					},
					"Clears": {
						"op": "add",
						"val": 1
					},
					"HighestStuns": {
						"op": "max",
						"val": 3
					},
					"Passes": {
						"op": "add",
						"val": 1
					},
					"PossessionTime": {
						"op": "add",
						"val": 8.9664583
					},
					"PunchesReceived": {
						"op": "add",
						"val": 6
					},
					"ShotsOnGoalAgainst": {
						"op": "add",
						"val": 18
					},
					"Stuns": {
						"op": "add",
						"val": 3
					},
					"StunsPerGame": {
						"op": "rep",
						"val": 3
					},
					"TopSpeedsTotal": {
						"op": "add",
						"val": 47.514111
					},
					"XP": {
						"op": "add",
						"val": 1000
					}
				},
				"weekly_2024_04_08": {
					"ArenaLosses": {
						"op": "add",
						"val": 1
					},
					"AveragePossessionTimePerGame": {
						"op": "rep",
						"val": 8.9664583
					},
					"AverageTopSpeedPerGame": {
						"op": "rep",
						"val": 47.514111
					},
					"Catches": {
						"op": "add",
						"val": 1
					},
					"Clears": {
						"op": "add",
						"val": 1
					},
					"HighestStuns": {
						"op": "max",
						"val": 3
					},
					"Passes": {
						"op": "add",
						"val": 1
					},
					"PossessionTime": {
						"op": "add",
						"val": 8.9664583
					},
					"PunchesReceived": {
						"op": "add",
						"val": 6
					},
					"ShotsOnGoalAgainst": {
						"op": "add",
						"val": 18
					},
					"Stuns": {
						"op": "add",
						"val": 3
					},
					"StunsPerGame": {
						"op": "rep",
						"val": 3
					},
					"TopSpeedsTotal": {
						"op": "add",
						"val": 47.514111
					},
					"XP": {
						"op": "add",
						"val": 1000
					}
				}
			}
		}
	}`
	var update evr.UpdatePayload
	err := json.Unmarshal([]byte(jsonData), &update)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}
	data, err := json.Marshal(update.Update.StatsGroups)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}
	profile := evr.ServerProfile{}
	err = json.Unmarshal([]byte(data), &profile.Statistics)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}
	t.Errorf("%v", profile.Statistics)
	tests := []struct {
		name    string
		args    args
		want    *GameProfileData
		wantErr bool
	}{
		{
			"update data",
			args{
				logger: zap.NewNop(),
				profile: &GameProfileData{
					Server: evr.ServerProfile{},
				},
				update: update,
			},
			&GameProfileData{
				Server: evr.ServerProfile{},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			/*
				got, err := updateProfileStats(tt.args.logger, tt.args.profile, tt.args.update)

				if (err != nil) != tt.wantErr {
					t.Errorf("updateProfileStats() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("updateProfileStats() = %v, want %v", got, tt.want)
				}
			*/
		})
	}
}
