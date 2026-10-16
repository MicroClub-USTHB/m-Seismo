// Package audit records state changes in audit_logs. Services call Record
// inside the same transaction as the change, so a change is never committed
// without its audit row.
package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type actorKey struct{}

// WithActor stores who performs the request in ctx.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom returns the actor stored in ctx, or "system".
func ActorFrom(ctx context.Context) string {
	if a, ok := ctx.Value(actorKey{}).(string); ok && a != "" {
		return a
	}
	return "system"
}

type Entry struct {
	UserID     *uuid.UUID // owning user, if any
	Action     string     // e.g. "user.create"
	EntityType string
	EntityID   uuid.UUID
	Payload    any // marshalled to JSON; must not contain secrets
}

func Record(ctx context.Context, q db.Querier, e Entry) error {
	_, err := q.Exec(ctx, `
		INSERT INTO audit_logs (user_id, actor, action, entity_type, entity_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.UserID, ActorFrom(ctx), e.Action, e.EntityType, e.EntityID, e.Payload)
	if err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Action, err)
	}
	return nil
}
