// Package migrate hosts one-shot data migrations that don't belong in the
// normal request path or the long-running server. Each migration is a small,
// testable unit driven by a thin cmd/* wrapper.
package migrate

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/WithAutonomi/indelible/internal/services"
)

// emit writes one JSONLines record to w. Best-effort: if w is nil we skip;
// if encoding fails we drop silently because progress output isn't load-bearing.
func emit(w io.Writer, row RowResult) {
	if w == nil {
		return
	}
	b, err := json.Marshal(row)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

// ChunkPublisher is the subset of antd-go the publish-datamaps migration needs.
// Decoupled from *antd.Client so tests can supply a fake.
type ChunkPublisher interface {
	ChunkPut(ctx context.Context, data []byte) (address string, err error)
	ChunkGet(ctx context.Context, address string) (data []byte, err error)
}

// PublishDataMapsOptions configures a run.
type PublishDataMapsOptions struct {
	DryRun bool   // list candidates and exit without publishing
	Limit  int    // 0 = unlimited
	UUID   string // single-row: only this upload (still subject to candidate filter)
	Verify bool   // round-trip ChunkGet after publish and assert bytes match
}

// RowResult records what happened for one candidate row. Emitted to the
// JSONLines progress writer.
type RowResult struct {
	UUID           string `json:"uuid"`
	ID             int64  `json:"id"`
	Outcome        string `json:"outcome"` // "published", "verified", "skipped", "failed", "dry_run"
	DatamapAddress string `json:"datamap_address,omitempty"`
	Error          string `json:"error,omitempty"`
	DurationMs     int64  `json:"duration_ms,omitempty"`
}

// PublishDataMapsRun is the run summary returned to callers (and printed at end).
type PublishDataMapsRun struct {
	Candidates int         `json:"candidates"`
	Published  int         `json:"published"`
	Verified   int         `json:"verified"`
	Failed     int         `json:"failed"`
	DryRun     bool        `json:"dry_run"`
	Rows       []RowResult `json:"rows,omitempty"`
}

// ErrDaemonWalletMissing surfaces the specific antd error that this migration
// most often hits: antd was started without AUTONOMI_WALLET_KEY. We promote it
// to a sentinel so the caller can abort the run loudly instead of grinding
// through 78 identical 503s.
var ErrDaemonWalletMissing = errors.New("antd has no daemon wallet (set AUTONOMI_WALLET_KEY) — see runbook")

// PublishDataMaps republishes the locally-stored DataMaps of completed private
// uploads as on-network chunks, flipping the row to visibility='public' with
// the resulting datamap_address. Idempotent: ant-core's chunk_put is
// content-addressed, so re-running on a partially-completed batch yields the
// same addresses and the loop just re-fills any rows whose UPDATE didn't land.
//
// progress, if non-nil, receives one JSON-encoded RowResult per line as the
// run progresses. The final PublishDataMapsRun is returned to the caller.
func PublishDataMaps(
	ctx context.Context,
	uploads *services.UploadService,
	publisher ChunkPublisher,
	opts PublishDataMapsOptions,
	progress io.Writer,
) (*PublishDataMapsRun, error) {
	candidates, err := uploads.ListPrivatePublishCandidates(opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	if opts.UUID != "" {
		filtered := candidates[:0]
		for _, u := range candidates {
			if u.UUID == opts.UUID {
				filtered = append(filtered, u)
			}
		}
		candidates = filtered
	}

	run := &PublishDataMapsRun{
		Candidates: len(candidates),
		DryRun:     opts.DryRun,
	}

	for _, u := range candidates {
		if err := ctx.Err(); err != nil {
			return run, err
		}

		row := RowResult{UUID: u.UUID, ID: u.ID}

		if opts.DryRun {
			row.Outcome = "dry_run"
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		if !u.DataMap.Valid || u.DataMap.String == "" {
			row.Outcome = "skipped"
			row.Error = "no data_map"
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		raw, decodeErr := hex.DecodeString(u.DataMap.String)
		if decodeErr != nil {
			row.Outcome = "failed"
			row.Error = "hex decode: " + decodeErr.Error()
			run.Failed++
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		start := time.Now()
		addr, putErr := publisher.ChunkPut(ctx, raw)
		row.DurationMs = time.Since(start).Milliseconds()
		if putErr != nil {
			// The first daemon-wallet-missing failure aborts the whole run —
			// every subsequent row will hit the same 503 and there's no point
			// burning 78 noisy errors for the operator to scroll through.
			if isWalletMissing(putErr) {
				row.Outcome = "failed"
				row.Error = putErr.Error()
				run.Failed++
				emit(progress, row)
				run.Rows = append(run.Rows, row)
				return run, ErrDaemonWalletMissing
			}
			row.Outcome = "failed"
			row.Error = "chunk_put: " + putErr.Error()
			run.Failed++
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		row.DatamapAddress = addr

		if opts.Verify {
			got, getErr := publisher.ChunkGet(ctx, addr)
			if getErr != nil {
				row.Outcome = "failed"
				row.Error = "verify chunk_get: " + getErr.Error()
				run.Failed++
				emit(progress, row)
				run.Rows = append(run.Rows, row)
				continue
			}
			if !bytes.Equal(got, raw) {
				row.Outcome = "failed"
				row.Error = fmt.Sprintf("verify mismatch: published=%d bytes, fetched=%d bytes", len(raw), len(got))
				run.Failed++
				emit(progress, row)
				run.Rows = append(run.Rows, row)
				continue
			}
		}

		if err := uploads.MarkPublished(u.ID, addr); err != nil {
			row.Outcome = "failed"
			row.Error = "db update: " + err.Error()
			run.Failed++
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		if opts.Verify {
			row.Outcome = "verified"
			run.Verified++
		} else {
			row.Outcome = "published"
		}
		run.Published++
		emit(progress, row)
		run.Rows = append(run.Rows, row)
	}

	return run, nil
}

// isWalletMissing pattern-matches the antd 503 body
// ("wallet not configured — set AUTONOMI_WALLET_KEY"). antd-go surfaces this
// as ServiceUnavailableError; we string-match instead of importing antd's
// error types because the message is the actual contract here and a future
// antd refactor that keeps the message but changes the type would still want
// the same UX.
func isWalletMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "wallet not configured") ||
		strings.Contains(msg, "autonomi_wallet_key")
}
