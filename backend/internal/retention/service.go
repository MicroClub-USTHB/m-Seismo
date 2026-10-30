// Package retention deletes reports once they outlive their app's
// retention period (PRD §8), a day at a time, independent of attachments
// already removed through the report package.
package retention

import (
	"context"
	"fmt"
	"log/slog"
)

// Deleter removes expired reports. Implemented by db.Querier.
type Deleter interface {
	DeleteExpiredReports(ctx context.Context, defaultDays int32) (int64, error)
}

type Service struct {
	q           Deleter
	defaultDays int32
}

func NewService(q Deleter, defaultDays int) *Service {
	return &Service{q: q, defaultDays: int32(defaultDays)}
}

// Sweep deletes every report (and, through the cascade, its attachments)
// older than its app's retention_days, or defaultDays when the app has
// none set. It returns how many reports were deleted.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	n, err := s.q.DeleteExpiredReports(ctx, s.defaultDays)
	if err != nil {
		return 0, fmt.Errorf("retention: sweep: %w", err)
	}
	if n > 0 {
		slog.InfoContext(ctx, "retention: swept expired reports", "deleted", n)
	}
	return n, nil
}
