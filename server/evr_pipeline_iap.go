package server

import (
	"context"
	"fmt"

	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

// ReconcileIAP reconciles an in-app purchase. This is a stub implementation.
func (p *EvrPipeline) reconcileIAP(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.ReconcileIAP)
	if err := session.SendEvr(
		evr.NewReconcileIAPResult(request.EvrID),
	); err != nil {
		return fmt.Errorf("failed to send ReconcileIAPResult: %v", err)
	}
	return nil
}
