package server

import (
	"net"
	"testing"
)

func Test_ipToKey(t *testing.T) {
	type args struct {
		ip net.IP
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "IPv4",
			args: args{
				ip: net.ParseIP("192.168.1.1"),
			},
			want: "rttc0a80101",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ipToKey(tt.args.ip); got != tt.want {
				t.Errorf("ipToKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_distributeParties(t *testing.T) {
	partyA1 := &MatchmakingSession{}
	partyA2 := &MatchmakingSession{}
	partyA3 := &MatchmakingSession{}
	partyB1 := &MatchmakingSession{}
	partyB2 := &MatchmakingSession{}
	partyB3 := &MatchmakingSession{}
	partyC1 := &MatchmakingSession{}
	partyD1 := &MatchmakingSession{}
	type args struct {
		parties [][]*MatchmakingSession
	}
	tests := []struct {
		name string
		args args
		want [][]*MatchmakingSession
	}{
		{
			name: "Test Case 1",
			args: args{
				parties: [][]*MatchmakingSession{
					{
						partyA1,
						partyA2,
						partyA3,
					},
					{
						partyB1,
						partyB2,
						partyB3,
					},
					{
						partyC1,
					},
					{
						partyD1,
					},
				},
			},
			want: [][]*MatchmakingSession{
				{
					partyA1,
					partyA2,
					partyA3,
					partyC1,
				},
				{
					partyB1,
					partyB2,
					partyB3,
					partyD1,
				},
			},
		},
		// Add more test cases here
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rosters := distributeParties(tt.args.parties)
			for i, team := range rosters {
				for j, player := range team {
					if player != tt.want[i][j] {
						t.Errorf("distributeParties() = %v, want %v", player, tt.want[i][j])
					}
				}
			}
		})
	}
}
