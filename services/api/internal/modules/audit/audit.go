// Package audit appends to audit_events, the append-only journal shared by candidates,
// administrators and system actions. Metadata carries technical identifiers only: never a
// name, email, phone number, password, grade or clear IP address.
package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ActorCandidate = "CANDIDATE"
	ActorAdmin     = "ADMIN"
	ActorSystem    = "SYSTEM"
)

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Actor is who acted; ID is empty for an unauthenticated or system actor.
type Actor struct {
	Type string
	ID   string
}

// Record appends one event; targetType and targetID are both empty or both set.
func Record(ctx context.Context, db Execer, eventType string, actor Actor, targetType, targetID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO audit_events(event_type,actor_type,actor_id,target_type,target_id,metadata)
	 VALUES($1,$2,nullif($3,'')::uuid,nullif($4,''),nullif($5,'')::uuid,$6::jsonb)`, eventType, actor.Type, actor.ID, targetType, targetID, body)
	return err
}

// AccessEvent is written for every consultation of personal data by an administrator.
const AccessEvent = "ADMIN_DATA_ACCESSED"

// RecordAccess traces that an administrator opened a record (phase 8: each application view),
// with the reason of the access, inside the transaction that reads the data.
func RecordAccess(ctx context.Context, db Execer, adminID, targetType, targetID, purpose string) error {
	return Record(ctx, db, AccessEvent, Actor{Type: ActorAdmin, ID: adminID}, targetType, targetID, map[string]any{"purpose": purpose})
}
