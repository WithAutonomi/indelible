package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	antd "github.com/maidsafe/ant-sdk/antd-go"

	"github.com/maidsafe/indelible/internal/config"
	"github.com/maidsafe/indelible/internal/services"
)

// UploadWorker processes queued file uploads in the background.
type UploadWorker struct {
	uploadSvc  *services.UploadService
	txnSvc     *services.TransactionService
	walletSvc  *services.WalletService
	antdClient *antd.Client
	cfg        *config.Config
	wg         sync.WaitGroup
	cancel     context.CancelFunc
}

// NewUploadWorker creates a new background upload processor.
func NewUploadWorker(db *sql.DB, cfg *config.Config) *UploadWorker {
	return &UploadWorker{
		uploadSvc:  services.NewUploadService(db),
		txnSvc:     services.NewTransactionService(db),
		walletSvc:  services.NewWalletService(db, cfg.WalletEncryptionKey),
		antdClient: antd.NewClient(cfg.AntdURL),
		cfg:        cfg,
	}
}

// Start begins the upload processing loop and the stuck-upload reconciliation loop.
func (w *UploadWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	// Requeue any uploads left in "processing" from a previous crash
	requeued, err := w.uploadSvc.RequeueStuck(0) // immediate: anything still "processing" on startup
	if err != nil {
		slog.Error("crash recovery requeue failed", "error", err)
	} else if requeued > 0 {
		slog.Info("crash recovery: requeued stuck uploads", "count", requeued)
	}

	// Main processing loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.processLoop(ctx)
	}()

	// Reconciliation loop (check for stuck uploads every 5 minutes)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.reconcileLoop(ctx)
	}()

	slog.Info("upload worker started")
}

// Stop gracefully shuts down the worker, waiting for in-flight uploads.
func (w *UploadWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("upload worker stopped")
}

func (w *UploadWorker) processLoop(ctx context.Context) {
	// Poll interval — when idle, check every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processNext(ctx)
		}
	}
}

func (w *UploadWorker) processNext(ctx context.Context) {
	upload, err := w.uploadSvc.DequeueNext()
	if err != nil {
		slog.Error("dequeue upload failed", "error", err)
		return
	}
	if upload == nil {
		return // nothing to process
	}

	slog.Info("processing upload", "uuid", upload.UUID, "filename", upload.OriginalFilename, "size", upload.FileSize)

	// Process the upload
	if err := w.processUpload(ctx, upload); err != nil {
		slog.Error("upload failed", "uuid", upload.UUID, "error", err)
		w.uploadSvc.MarkFailed(upload.ID, err.Error())
		w.cleanupTempFile(upload)
		return
	}

	w.cleanupTempFile(upload)
}

func (w *UploadWorker) processUpload(ctx context.Context, upload *services.Upload) error {
	if !upload.TempPath.Valid || upload.TempPath.String == "" {
		return fmt.Errorf("no temp file path for upload %s", upload.UUID)
	}

	tempPath := upload.TempPath.String

	// Verify temp file exists
	if _, err := os.Stat(tempPath); err != nil {
		return fmt.Errorf("temp file missing: %w", err)
	}

	var result *antd.PutResult
	var err error

	if upload.Visibility == "public" {
		result, err = w.antdClient.FileUploadPublic(ctx, tempPath)
	} else {
		// Private upload: read file bytes and use DataPutPrivate
		data, readErr := os.ReadFile(tempPath)
		if readErr != nil {
			return fmt.Errorf("reading temp file: %w", readErr)
		}
		result, err = w.antdClient.DataPutPrivate(ctx, data)
	}

	if err != nil {
		return fmt.Errorf("antd upload: %w", err)
	}

	// Mark upload completed
	if err := w.uploadSvc.MarkCompleted(upload.ID, result.Address, result.Cost); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	// Record transaction against default wallet (best-effort)
	if wallet, err := w.walletSvc.GetDefault(); err == nil {
		w.txnSvc.Record(wallet.ID, &upload.ID, "upload", result.Cost, wallet.PaymentBalance)
	}

	slog.Info("upload completed", "uuid", upload.UUID, "address", result.Address, "cost", result.Cost)
	return nil
}

func (w *UploadWorker) cleanupTempFile(upload *services.Upload) {
	if !upload.TempPath.Valid || upload.TempPath.String == "" {
		return
	}
	if err := os.Remove(upload.TempPath.String); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to clean up temp file", "path", upload.TempPath.String, "error", err)
	}
}

func (w *UploadWorker) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requeued, err := w.uploadSvc.RequeueStuck(2) // stuck > 2 minutes
			if err != nil {
				slog.Error("reconciliation requeue failed", "error", err)
			} else if requeued > 0 {
				slog.Info("reconciliation: requeued stuck uploads", "count", requeued)
			}
		}
	}
}

// TempUploadDir returns the path to the temp upload directory, creating it if needed.
func TempUploadDir(cfg *config.Config) string {
	dir := filepath.Join(cfg.DataDir, "uploads", "tmp")
	os.MkdirAll(dir, 0750)
	return dir
}
