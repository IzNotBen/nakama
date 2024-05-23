package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
)

func TestEvrMatch_matchState(t *testing.T) {
	type args struct {
		data string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "matchStateUnmarshal",
			args: args{
				data: `{"id":"7aab54ba-90ae-4e7f-abcf-69b30f5e8db7","open":true,"lobby_type":"public","endpoint":"","version_lock":14280634968751706381,"platform":"OVR","channel":"e8dd7736-32af-41f5-91e0-db591c6e8cfd","match_channels":["e8dd7736-32af-41f5-91e0-db591c6e8cfd","c016925b-3368-401c-8620-0c4ccd7e5c2e","f52129fb-d5c6-4c47-b644-f19981a933ee"],"mode":"social_2.0","level":"mpl_lobby_b2","session_settings":{"appid":"1369078409873402","gametype":301069346851901300},"max_size":15,"size":14,"max_team_size":15,"Presences":null}`,
			},
			want: `{"id":"7aab54ba-90ae-4e7f-abcf-69b30f5e8db7","open":true,"lobby_type":"public","endpoint":"","version_lock":14280634968751706381,"platform":"OVR","channel":"e8dd7736-32af-41f5-91e0-db591c6e8cfd","match_channels":["e8dd7736-32af-41f5-91e0-db591c6e8cfd","c016925b-3368-401c-8620-0c4ccd7e5c2e","f52129fb-d5c6-4c47-b644-f19981a933ee"],"mode":"social_2.0","level":"mpl_lobby_b2","session_settings":{"appid":"1369078409873402","gametype":301069346851901300},"max_size":15,"size":14,"max_team_size":15,"Presences":null}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO protobuf's would be nice here.
			got := &matchState{}
			err := json.Unmarshal([]byte(tt.args.data), got)
			if err != nil {
				t.Fatalf("error unmarshalling data: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrMatch.MatchSignal() got = %s\n\nwant = %s", got.String(), tt.want)
			}

		})
	}
}

func TestSelectTeamForPlayer(t *testing.T) {
	presencesstr := map[string]*PlayerPresence{
		"player1": {Alignment: evr.BlueTeamRole},
		"player2": {Alignment: evr.OrangeTeamRole},
		"player3": {Alignment: evr.SpectatorRole},
		"player4": {Alignment: evr.OrangeTeamRole},
		"player5": {Alignment: evr.OrangeTeamRole},
	}

	presences := make(map[evr.GUID]*PlayerPresence)
	for k, v := range presencesstr {
		u := evr.GUID(uuid.NewV5(uuid.Nil, k))
		presences[u] = v
	}

	state := &matchState{
		Presences: presences,
	}

	tests := []struct {
		name           string
		preferred      evr.Role
		lobbyType      evr.LobbyType
		presences      map[string]*PlayerPresence
		expectedRole   evr.Role
		expectedResult bool
	}{
		{
			name:           "UnassignedPlayer",
			lobbyType:      evr.PrivateLobby,
			preferred:      evr.UnassignedRole,
			presences:      map[string]*PlayerPresence{},
			expectedRole:   evr.BlueTeamRole,
			expectedResult: true,
		},
		{
			name:      "Public match, blue team full, puts the player on orange",
			lobbyType: evr.PublicLobby,
			preferred: evr.BlueTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.OrangeTeamRole},
				"player6": {Alignment: evr.SpectatorRole},
				"player7": {Alignment: evr.OrangeTeamRole},
				"player8": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.OrangeTeamRole,
			expectedResult: true,
		},
		{
			name:      "Public match, orange team full, puts the player on blue",
			lobbyType: evr.PublicLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.SpectatorRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.OrangeTeamRole},
				"player6": {Alignment: evr.OrangeTeamRole},
				"player7": {Alignment: evr.OrangeTeamRole},
				"player8": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.BlueTeamRole,
			expectedResult: true,
		},
		{
			name:      "Public match, teams equal, use preference",
			lobbyType: evr.PublicLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.SpectatorRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.OrangeTeamRole},
				"player6": {Alignment: evr.OrangeTeamRole},
				"player7": {Alignment: evr.OrangeTeamRole},
				"player8": {Alignment: evr.SpectatorRole},
			},
			expectedRole:   evr.OrangeTeamRole,
			expectedResult: true,
		},
		{
			name:      "Public match, full reject",
			lobbyType: evr.PublicLobby,
			preferred: evr.BlueTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.OrangeTeamRole},
				"player6": {Alignment: evr.OrangeTeamRole},
				"player7": {Alignment: evr.OrangeTeamRole},
				"player8": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.UnassignedRole,
			expectedResult: false,
		},
		{
			name:      "Public match, spectators full, reject",
			lobbyType: evr.PublicLobby,
			preferred: evr.SpectatorRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.SpectatorRole},
				"player4": {Alignment: evr.SpectatorRole},
				"player5": {Alignment: evr.SpectatorRole},
				"player6": {Alignment: evr.SpectatorRole},
				"player7": {Alignment: evr.SpectatorRole},
				"player8": {Alignment: evr.SpectatorRole},
			},
			expectedRole:   evr.UnassignedRole,
			expectedResult: false,
		},
		{
			name:      "Private match, use preference",
			lobbyType: evr.PrivateLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.SpectatorRole},
				"player6": {Alignment: evr.SpectatorRole},
				"player7": {Alignment: evr.SpectatorRole},
				"player8": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.OrangeTeamRole,
			expectedResult: true,
		},
		{
			name:      "Private match, use preference (5 player teams)",
			lobbyType: evr.PrivateLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.SpectatorRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.BlueTeamRole},
				"player4": {Alignment: evr.BlueTeamRole},
				"player5": {Alignment: evr.OrangeTeamRole},
				"player6": {Alignment: evr.OrangeTeamRole},
				"player7": {Alignment: evr.OrangeTeamRole},
				"player8": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.OrangeTeamRole,
			expectedResult: true,
		},
		{
			name:      "Private match, preference full, put on other team",
			lobbyType: evr.PrivateLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1":  {Alignment: evr.SpectatorRole},
				"player2":  {Alignment: evr.BlueTeamRole},
				"player3":  {Alignment: evr.BlueTeamRole},
				"player4":  {Alignment: evr.BlueTeamRole},
				"player5":  {Alignment: evr.BlueTeamRole},
				"player6":  {Alignment: evr.OrangeTeamRole},
				"player7":  {Alignment: evr.OrangeTeamRole},
				"player8":  {Alignment: evr.OrangeTeamRole},
				"player9":  {Alignment: evr.OrangeTeamRole},
				"player10": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.BlueTeamRole,
			expectedResult: true,
		},
		{
			name:      "Full private match, puts the player on spectator",
			lobbyType: evr.PrivateLobby,
			preferred: evr.OrangeTeamRole,
			presences: map[string]*PlayerPresence{
				"player1":  {Alignment: evr.BlueTeamRole},
				"player2":  {Alignment: evr.BlueTeamRole},
				"player3":  {Alignment: evr.BlueTeamRole},
				"player4":  {Alignment: evr.BlueTeamRole},
				"player5":  {Alignment: evr.BlueTeamRole},
				"player6":  {Alignment: evr.OrangeTeamRole},
				"player7":  {Alignment: evr.OrangeTeamRole},
				"player8":  {Alignment: evr.OrangeTeamRole},
				"player9":  {Alignment: evr.OrangeTeamRole},
				"player10": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.SpectatorRole,
			expectedResult: true,
		},
		{
			name:      "Private match, spectators full, reject",
			lobbyType: evr.PrivateLobby,
			preferred: evr.SpectatorRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.SpectatorRole},
				"player4": {Alignment: evr.SpectatorRole},
				"player5": {Alignment: evr.SpectatorRole},
				"player6": {Alignment: evr.SpectatorRole},
				"player7": {Alignment: evr.SpectatorRole},
				"player8": {Alignment: evr.SpectatorRole},
			},
			expectedRole:   evr.UnassignedRole,
			expectedResult: false,
		},
		{
			name:      "full social lobby, reject",
			lobbyType: evr.PublicLobby,
			preferred: evr.SpectatorRole,
			presences: map[string]*PlayerPresence{
				"player1":  {Alignment: evr.SocialRole},
				"player2":  {Alignment: evr.SocialRole},
				"player3":  {Alignment: evr.SocialRole},
				"player4":  {Alignment: evr.SocialRole},
				"player5":  {Alignment: evr.SocialRole},
				"player6":  {Alignment: evr.SocialRole},
				"player7":  {Alignment: evr.SocialRole},
				"player8":  {Alignment: evr.SocialRole},
				"player9":  {Alignment: evr.SocialRole},
				"player10": {Alignment: evr.SocialRole},
				"player11": {Alignment: evr.SocialRole},
				"player12": {Alignment: evr.SocialRole},
			},
			expectedRole:   evr.UnassignedRole,
			expectedResult: false,
		},
		{
			name:      "social lobby, moderator, allow",
			lobbyType: evr.PublicLobby,
			preferred: evr.ModeratorRole,
			presences: map[string]*PlayerPresence{
				"player1":  {Alignment: evr.SocialRole},
				"player2":  {Alignment: evr.SocialRole},
				"player3":  {Alignment: evr.SocialRole},
				"player4":  {Alignment: evr.SocialRole},
				"player5":  {Alignment: evr.SocialRole},
				"player6":  {Alignment: evr.SocialRole},
				"player7":  {Alignment: evr.SocialRole},
				"player8":  {Alignment: evr.SocialRole},
				"player9":  {Alignment: evr.SocialRole},
				"player10": {Alignment: evr.SocialRole},
				"player11": {Alignment: evr.SocialRole},
			},
			expectedRole:   evr.ModeratorRole,
			expectedResult: true,
		},
		{
			name:      "Blue team unbalanced, put on orange",
			lobbyType: evr.PublicLobby,
			preferred: evr.BlueTeamRole,
			presences: map[string]*PlayerPresence{
				"player1": {Alignment: evr.BlueTeamRole},
				"player2": {Alignment: evr.BlueTeamRole},
				"player3": {Alignment: evr.OrangeTeamRole},
			},
			expectedRole:   evr.OrangeTeamRole,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		presence := &PlayerPresence{
			Alignment: tt.preferred,
		}
		presences := make(map[uuid.UUID]*PlayerPresence)
		for k, v := range tt.presences {
			u := uuid.NewV5(uuid.Nil, k)
			presences[u] = v
		}
		state.Presences = presences
		state.Settings = NewMatchSettingsFromMode(evr.ModeSocialPublic, evr.Symbol(VersionLock))

		t.Run(tt.name, func(t *testing.T) {
			team, result := selectPlayerRole(NewRuntimeGoLogger(logger), presence.Alignment, state)

			if team != tt.expectedRole {
				t.Errorf("selectTeamForPlayer() returned incorrect team, got: %d, want: %d", team, tt.expectedRole)
			}

			if result != tt.expectedResult {
				t.Errorf("selectTeamForPlayer() returned incorrect result, got: %t, want: %t", result, tt.expectedResult)
			}
		})
	}
}

func TestSelectTeamForPlayer_With_Alighment(t *testing.T) {
	const (
		DMO_1  = "DMO-1"
		DMO_2  = "DMO-2"
		DMO_3  = "DMO-3"
		DMO_4  = "DMO-4"
		DMO_5  = "DMO-5"
		DMO_6  = "DMO-6"
		DMO_7  = "DMO-7"
		DMO_8  = "DMO-8"
		DMO_9  = "DMO-9"
		DMO_10 = "DMO-10"
		DMO_11 = "DMO-11"
		DMO_12 = "DMO-12"
		DMO_13 = "DMO-13"

		Blue       = evr.BlueTeamRole
		Orange     = evr.OrangeTeamRole
		Spectator  = evr.SpectatorRole
		Unassigned = evr.UnassignedRole
	)
	alignments := map[string]evr.Role{
		DMO_1:  Blue,
		DMO_2:  Blue,
		DMO_3:  Blue,
		DMO_4:  Blue,
		DMO_5:  Orange,
		DMO_6:  Orange,
		DMO_7:  Orange,
		DMO_8:  Orange,
		DMO_9:  Spectator,
		DMO_10: Spectator,
		DMO_11: Spectator,
		DMO_12: Spectator,
		DMO_13: Spectator,
	}

	tests := []struct {
		name          string
		mode          evr.Symbol
		players       []string
		newPlayer     string
		preferredTeam evr.Role
		expectedTeam  evr.Role
		allowed       bool
	}{
		{
			name: "Follows alignment even when unbalanced",
			mode: evr.ModeArenaPublic,
			players: []string{
				DMO_1,
				DMO_2,
				DMO_3,
			},
			newPlayer:     DMO_4,
			preferredTeam: evr.OrangeTeamRole,
			expectedTeam:  evr.BlueTeamRole,
			allowed:       true,
		},
	}

	for _, tt := range tests {
		// Existing players
		presences := make(map[uuid.UUID]*PlayerPresence)
		for _, player := range tt.players {
			u := uuid.NewV5(uuid.Nil, player)
			presences[u] = &PlayerPresence{
				Alignment: alignments[player],
			}
		}

		// New Player
		presence := &PlayerPresence{
			EvrID:     *lo.Must(evr.ParseEvrId(tt.newPlayer)),
			Alignment: tt.preferredTeam,
		}

		// Match State
		state := &matchState{
			Presences: presences,
			Settings:  NewMatchSettingsFromMode(tt.mode, evr.Symbol(VersionLock)),
		}

		t.Run(tt.name, func(t *testing.T) {
			team, result := selectPlayerRole(NewRuntimeGoLogger(logger), presence.Alignment, state)

			if team != tt.expectedTeam {
				t.Errorf("selectTeamForPlayer() returned incorrect team, got: %d, want: %d", team, tt.expectedTeam)
			}

			if result != tt.allowed {
				t.Errorf("selectTeamForPlayer() returned incorrect result, got: %t, want: %t", result, tt.allowed)
			}
		})
	}
}
