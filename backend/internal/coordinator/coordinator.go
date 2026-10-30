// Package coordinator is the backend side of the agent protocol
// (spec/agent-protocol.md): it serves Coordinator.Attach, where each app's
// log coordinator dials in, and lets the pipeline ask that coordinator for
// server logs.
//
// A coordinator's stream lands on one backend instance, but the worker that
// needs logs can run on any instance. The two meet on NATS:
//
//	worker ──request──▶ seismo.coordinator.query.<app id> ──▶ instance holding the stream
//	                                                          │ QueryLogs on the stream
//	worker ◀──reply──── AggregateResult or AggregateError ◀───┘
//
// The instance holding an app's stream subscribes to its query subject, so
// a request with no responder means no coordinator is attached for the app.
// When a coordinator attaches, its instance announces it on
// seismo.coordinator.attached.<app id>, and an older stream for the same app
// on any instance is closed: one live stream per app.
package coordinator

import (
	"github.com/google/uuid"
)

// ProtocolVersion is the agent protocol version this backend speaks.
const ProtocolVersion = 1

// maxMessageBytes caps gRPC messages on the Attach stream.
const maxMessageBytes = 10 << 20

func querySubject(appID uuid.UUID) string { return "seismo.coordinator.query." + appID.String() }

func metricsQuerySubject(appID uuid.UUID) string {
	return "seismo.coordinator.query.metrics." + appID.String()
}

func statusQuerySubject(appID uuid.UUID) string {
	return "seismo.coordinator.query.status." + appID.String()
}

func attachedSubject(appID uuid.UUID) string {
	return "seismo.coordinator.attached." + appID.String()
}

const attachedWildcard = "seismo.coordinator.attached.*"
