package handlers

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/WithAutonomi/indelible/internal/services"
)

// auditEvent is the canonical shorthand for handler-side audit writes. It
// pulls the chi-generated request ID (so all audit rows produced by one
// request can be pivoted via X-Request-Id) and the client IP + User-Agent
// out of *http.Request — the three fields every audit row should carry
// when produced inside an HTTP handler.
//
// userID is *int64 because some events fire before we know who the actor is
// (rate-limited login attempts, SSO callbacks that fail with no_account).
// Pass nil in those cases.
//
// V2-314: convention is to ignore the write error — audit logging should
// never fail a request.
func auditEvent(r *http.Request, logSvc *services.LogService, eventType, severity string, userID *int64, detail string) {
	_ = logSvc.WriteAudit(
		eventType, severity, userID, detail,
		r.RemoteAddr, r.UserAgent(), chimw.GetReqID(r.Context()),
	)
}

// fileAccessEvent is the file-read counterpart to auditEvent (V2-514). It writes
// to the plain, append-only file_access_log instead of the tamper-evident
// audit_log hash-chain — used for high-volume download-route telemetry
// (file_downloaded, file_download_denied) that a reader fleet emits, so reader
// replicas never serialize on or fork the audit chain. Same fire-and-forget
// contract as auditEvent: the write error is intentionally ignored.
func fileAccessEvent(r *http.Request, logSvc *services.LogService, eventType, severity string, userID *int64, detail string) {
	_ = logSvc.WriteFileAccess(
		eventType, severity, userID, detail,
		r.RemoteAddr, r.UserAgent(), chimw.GetReqID(r.Context()),
	)
}
