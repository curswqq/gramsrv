package rpc

import (
	"context"

	"go.uber.org/zap"

	"telesrv/internal/domain"
)

type accountRatingRecomputer interface {
	Recompute(context.Context, int64) (domain.AccountRating, error)
}

// refreshAccountRatings is a post-commit best-effort refresh. A rating failure
// must never roll back or misreport the already committed economic operation.
func (r *Router) refreshAccountRatings(ctx context.Context, userIDs ...int64) {
	recomputer, ok := r.deps.AccountRatings.(accountRatingRecomputer)
	if !ok || recomputer == nil {
		return
	}
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		if _, err := recomputer.Recompute(ctx, userID); err != nil {
			r.log.Warn("refresh account rating after economic operation", append(r.contextLogFields(ctx), zap.Int64("rating_user_id", userID), zap.Error(err))...)
			continue
		}
		r.invalidateRPCProjectionForUser(userID)
	}
}
