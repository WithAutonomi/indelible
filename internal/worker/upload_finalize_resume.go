package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/services"
)

// finalizeResumeDelays are the waits before each repeat of a finalize that
// stored only part of an upload while antd kept the paid attempt (V2-1774).
// About 7.5 minutes in total, well inside antd's 1 h pending-upload TTL. A
// variable so tests can shorten it.
var finalizeResumeDelays = []time.Duration{
	15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute,
}

// finalizeWithResume runs finalize and, while antd reports a retryable partial
// store, calls it again: antd >= 0.14 keeps the paid attempt under the same
// upload_id, so the remainder stores against the same payment, with no
// re-prepare and no second payment (V2-1774). It stops when the store
// completes, on any other error, when the retries run out, or when
// ChunksFailed stops shrinking, and returns the last result and error.
func (w *UploadWorker) finalizeWithResume(
	ctx context.Context,
	upload *services.Upload,
	uploadID string,
	finalize func() (*antd.FinalizeUploadResult, error),
) (*antd.FinalizeUploadResult, error) {
	result, err := finalize()
	lastFailed := uint64(math.MaxUint64)
	for attempt := 0; err != nil && attempt < len(finalizeResumeDelays); attempt++ {
		var partial *antd.PartialUploadError
		if !errors.As(err, &partial) || !partial.Retryable {
			return result, err
		}
		if partial.ChunksFailed >= lastFailed {
			slog.Warn("finalize resume made no progress — stopping",
				"uuid", upload.UUID, "upload_id", uploadID, "chunks_failed", partial.ChunksFailed)
			return result, err
		}
		lastFailed = partial.ChunksFailed
		delay := finalizeResumeDelays[attempt]
		slog.Warn("finalize stored only part of the upload — retrying against the retained payment",
			"uuid", upload.UUID, "upload_id", uploadID, "chunks_stored", partial.ChunksStored,
			"chunks_failed", partial.ChunksFailed, "total_chunks", partial.TotalChunks,
			"attempt", attempt+1, "of", len(finalizeResumeDelays), "delay", delay)
		select {
		case <-ctx.Done():
			return result, err
		case <-time.After(delay):
		}
		w.heartbeat(upload, "finalize resume")
		result, err = finalize()
	}
	return result, err
}

// logIfPartialUpload logs the chunk accounting and retention flags when err
// carries antd's PARTIAL_UPLOAD detail (some batches/chunks stored, the rest
// failed). upload_id is what a manual reconciliation needs while antd may
// still hold the paid attempt.
func logIfPartialUpload(uuid, uploadID string, err error) {
	var partial *antd.PartialUploadError
	if errors.As(err, &partial) {
		slog.Warn("finalize stored only part of the upload",
			"uuid", uuid, "upload_id", uploadID, "chunks_stored", partial.ChunksStored,
			"chunks_failed", partial.ChunksFailed, "total_chunks", partial.TotalChunks,
			"retryable", partial.Retryable, "retention_known", partial.RetentionKnown)
	}
}

// waveFinalizeError wraps a wave-batch finalize failure for classifyFailure.
// A partial store whose retention antd didn't report must not be re-prepared
// or paid for again: antd may still hold the paid attempt, so the upload stops
// for reconciliation (errPaidNoRetry). Anything else keeps errFinalizeFailed:
// a retry re-prepares, stored chunks cost nothing, and refuseRepayment stops a
// second payment for any that are still unstored (V2-1774).
func waveFinalizeError(uploadID string, err error) error {
	var partial *antd.PartialUploadError
	if errors.As(err, &partial) && !partial.RetentionKnown {
		return fmt.Errorf("Failed to finalize upload: antd did not say whether it kept the paid attempt (upload_id %s); not paying again: %w",
			uploadID, errors.Join(errPaidNoRetry, err))
	}
	return fmt.Errorf("Failed to finalize upload: %w", errors.Join(errFinalizeFailed, err))
}
