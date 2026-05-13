// Package migrate hosts one-shot data migrations driven by thin cmd/* wrappers.
package migrate

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	sdk "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/services"
)

// ChunkPublisher is the subset of antd-go the migration needs. Decoupled from
// *sdk.Client so tests can supply a fake.
type ChunkPublisher interface {
	PrepareChunkUpload(ctx context.Context, content []byte) (*sdk.PrepareChunkResult, error)
	FinalizeChunkUpload(ctx context.Context, uploadID string, txHashes map[string]string) (string, error)
	ChunkGet(ctx context.Context, address string) ([]byte, error)
}

// EvmPayer covers the slice of indelible's evm.Signer the migration calls.
// Concrete impl is *evm.Signer; mocked in tests.
type EvmPayer interface {
	PayForQuotes(ctx context.Context, privateKeyHex string, payments []sdk.PaymentInfo,
		tokenAddress, dataPaymentsAddress string) (map[string]string, error)
}

// Options configures a run.
type Options struct {
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
	Outcome        string `json:"outcome"` // "published", "verified", "skipped", "already_stored", "failed", "dry_run"
	DatamapAddress string `json:"datamap_address,omitempty"`
	Error          string `json:"error,omitempty"`
	DurationMs     int64  `json:"duration_ms,omitempty"`
}

// Run is the summary returned to callers (and printed at end).
type Run struct {
	Candidates    int         `json:"candidates"`
	Published     int         `json:"published"` // includes verified + already_stored
	Verified      int         `json:"verified"`
	AlreadyStored int         `json:"already_stored"`
	Failed        int         `json:"failed"`
	DryRun        bool        `json:"dry_run"`
	Rows          []RowResult `json:"rows,omitempty"`
}

// PublishDataMaps republishes the locally-stored DataMaps of completed private
// uploads as on-network chunks, flipping the row to visibility='public' with
// the resulting datamap_address. Pays through the supplied EvmPayer using the
// supplied wallet key.
//
// Idempotent: antd's prepare returns already_stored=true for chunks that are
// already on-network, so re-running on a partially-completed batch fills only
// the rows whose UPDATE didn't land.
//
// progress, if non-nil, receives one JSON-encoded RowResult per line as the
// run progresses.
func PublishDataMaps(
	ctx context.Context,
	uploads *services.UploadService,
	publisher ChunkPublisher,
	payer EvmPayer,
	walletKey string,
	opts Options,
	progress io.Writer,
) (*Run, error) {
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

	run := &Run{Candidates: len(candidates), DryRun: opts.DryRun}

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
		addr, outcome, perRowErr := publishOne(ctx, publisher, payer, walletKey, raw, opts.Verify)
		row.DurationMs = time.Since(start).Milliseconds()
		row.DatamapAddress = addr
		if perRowErr != nil {
			row.Outcome = "failed"
			row.Error = perRowErr.Error()
			run.Failed++
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		if err := uploads.MarkPublished(u.ID, addr); err != nil {
			row.Outcome = "failed"
			row.Error = "db update: " + err.Error()
			run.Failed++
			emit(progress, row)
			run.Rows = append(run.Rows, row)
			continue
		}

		row.Outcome = outcome
		switch outcome {
		case "already_stored":
			run.AlreadyStored++
			run.Published++
		case "verified":
			run.Verified++
			run.Published++
		default:
			run.Published++
		}
		emit(progress, row)
		run.Rows = append(run.Rows, row)
	}

	return run, nil
}

// publishOne handles the per-chunk happy path: prepare → maybe pay → finalize → maybe verify.
// Returns (address, outcome-tag, err) where outcome is "already_stored", "verified", or "published".
func publishOne(
	ctx context.Context,
	publisher ChunkPublisher,
	payer EvmPayer,
	walletKey string,
	raw []byte,
	verify bool,
) (string, string, error) {
	prep, err := publisher.PrepareChunkUpload(ctx, raw)
	if err != nil {
		return "", "", fmt.Errorf("prepare: %w", err)
	}

	if prep.AlreadyStored {
		return prep.Address, "already_stored", nil
	}

	if prep.UploadID == "" || len(prep.Payments) == 0 {
		return "", "", errors.New("prepare returned no payment shape for a non-stored chunk")
	}

	txHashes, err := payer.PayForQuotes(ctx, walletKey, prep.Payments,
		prep.PaymentTokenAddress, prep.PaymentVaultAddress)
	if err != nil {
		return "", "", fmt.Errorf("pay: %w", err)
	}

	addr, err := publisher.FinalizeChunkUpload(ctx, prep.UploadID, txHashes)
	if err != nil {
		return "", "", fmt.Errorf("finalize: %w", err)
	}
	if addr != prep.Address {
		return "", "", fmt.Errorf("address mismatch: prepare=%s finalize=%s", prep.Address, addr)
	}

	if verify {
		got, err := publisher.ChunkGet(ctx, addr)
		if err != nil {
			return addr, "", fmt.Errorf("verify chunk_get: %w", err)
		}
		if !bytes.Equal(got, raw) {
			return addr, "", fmt.Errorf("verify mismatch: published=%d bytes, fetched=%d bytes", len(raw), len(got))
		}
		return addr, "verified", nil
	}

	return addr, "published", nil
}

// emit writes one JSONLines record to w. Best-effort.
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
