package server

import (
	"context"

	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

func beforeSessionRequestHook(ctx context.Context, logger *zap.Logger, userID, username string, vars map[string]string, expiry int64, sessionID, clientIP, clientPort, lang string, in evr.Message) (evr.Message, error) {

	// Authenticate the client

	return in, nil
}
