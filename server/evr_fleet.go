package server

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/heroiclabs/nakama-common/runtime"
)

var (
	ErrInternalError = fmt.Errorf("internal error")
)

func onMatchmakerMatched(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, entries []runtime.MatchmakerEntry) (string, error) {
	// Define notification codes
	notificationConnectionInfo := 111
	notificationCreateTimeout := 112
	notificationCreateFailed := 113

	// Register the Matchmaker Matched hook to find/create a GameLift game session

	// Define the maximum amount of players per game
	maxPlayers := 10

	// Find existing GameLift game sessions
	fm := nk.GetFleetManager()
	query := fmt.Sprintf("+values.playerCount:<=%v", maxPlayers-len(entries)) // Assuming a max match size of 10, find a match that has enough player spaces for the matched players
	limit := 1
	cursor := ""
	instances, _, err := fm.List(ctx, query, limit, cursor)
	if err != nil {
		logger.WithField("error", err.Error()).Error("failed to list gamelift instances")
		return "", ErrInternalError
	}
	userIds := make([]string, 0, len(entries))
	for _, entry := range entries {
		userIds = append(userIds, entry.GetPresence().GetUserId())
	}
	// If an instance was found, tell GameLift the players are joining the instance and then notify players with the connection details
	if len(instances) > 0 {
		instance := instances[0]

		joinInfo, err := fm.Join(ctx, instance.Id, userIds, nil)
		if err != nil {
			logger.WithField("error", err.Error()).Error("failed to join gamelift instance")
			return "", ErrInternalError
		}

		// Send connection details notifications to players
		for _, userId := range userIds {
			// Get the user's GameLift session ID
			sessionId := ""
			for _, sessionInfo := range joinInfo.SessionInfo {
				if sessionInfo.UserId == userId {
					sessionId = sessionInfo.SessionId
					break
				}
			}

			subject := "connection-info"
			content := map[string]interface{}{
				"IpAddress": joinInfo.InstanceInfo.ConnectionInfo.IpAddress,
				"DnsName":   joinInfo.InstanceInfo.ConnectionInfo.DnsName,
				"Port":      joinInfo.InstanceInfo.ConnectionInfo.Port,
				"SessionId": sessionId,
			}
			code := notificationConnectionInfo
			senderId := "" // System sender
			persistent := false
			nk.NotificationSend(ctx, userId, subject, content, code, senderId, persistent)
		}

		// We don't pass a Match ID back to the user as we are not creating a Nakama match
		return "", nil
	}

	// If no instance was found, ask GameLift to create a new one and, when it is available, notify the players with the connection details
	// First establish the creation callback
	var callback runtime.FmCreateCallbackFn = func(status runtime.FmCreateStatus, instanceInfo *runtime.InstanceInfo, sessionInfo []*runtime.SessionInfo, metadata map[string]any, createErr error) {
		switch status {
		case runtime.CreateSuccess:

			// Send connection details notifications to players
			for _, userId := range userIds {
				// Get the user's GameLift session ID
				sessionId := ""
				for _, sessionInfo := range sessionInfo {
					if sessionInfo.UserId == userId {
						sessionId = sessionInfo.SessionId
						break
					}
				}

				subject := "connection-info"
				content := map[string]interface{}{
					"IpAddress": instanceInfo.ConnectionInfo.IpAddress,
					"DnsName":   instanceInfo.ConnectionInfo.DnsName,
					"Port":      instanceInfo.ConnectionInfo.Port,
					"SessionId": sessionId,
				}
				code := notificationConnectionInfo
				senderId := "" // System sender
				persistent := false
				nk.NotificationSend(ctx, userId, subject, content, code, senderId, persistent)
			}
			return
		case runtime.CreateTimeout:
			logger.WithField("error", createErr.Error()).Error("Failed to create GameLift instance, timed out")

			// Send notification to client that game session creation timed out
			for _, userId := range userIds {
				subject := "create-timeout"
				content := map[string]interface{}{}
				code := notificationCreateTimeout
				senderId := "" // System sender
				persistent := false
				nk.NotificationSend(ctx, userId, subject, content, code, senderId, persistent)
			}
		default:
			logger.WithField("error", createErr.Error()).Error("Failed to create GameLift instance")

			// Send notification to client that game session couldn't be created
			for _, userId := range userIds {
				subject := "create-timeout"
				content := map[string]interface{}{}
				code := notificationCreateFailed
				senderId := "" // System sender
				persistent := false
				nk.NotificationSend(ctx, userId, subject, content, code, senderId, persistent)
			}
			return
		}
	}

	// Game session metadata as described by AWS GameLift Documentation
	// https://docs.aws.amazon.com/gamelift/latest/apireference/API_GameSession.html
	// These properties can be queried by the Fleet Manager when listing existing game sessions.
	// These properties can also be updated by calling the Update RPC from the headless server.
	metadata := map[string]interface{}{
		"GameSessionData": "<game_session_data>",
		"GameSessionName": "<game_session_name>",
		"GameProperties": map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	latencies := make([]runtime.FleetUserLatencies, 0, len(userIds))

	/*
		latencies := []runtime.FleetUserLatencies{{
			UserId:                userId,
			LatencyInMilliseconds: 100,
			RegionIdentifier:      "us-east-1",
		}}
	*/
	if err = fm.Create(ctx, maxPlayers, userIds, latencies, metadata, callback); err != nil {
		logger.WithField("error", err.Error()).Error("failed to create new fleet game session")
		return "", ErrInternalError
	}

	// We don't pass a Match ID back to the user as we are not creating a Nakama match
	return "", nil
}
