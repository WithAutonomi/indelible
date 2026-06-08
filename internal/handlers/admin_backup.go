package handlers

import (
	"fmt"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// @Summary      Export uploads (catalog + DataMaps)
// @Description  Stream the full upload catalog as NDJSON for disaster recovery or migration. SECRET-GRADE: the export contains every private DataMap (the locally-held retrieval secret for private uploads). Treat the downloaded file like a credential.
// @Tags         Admin: Backup
// @Produce      application/x-ndjson
// @Success      200 {file} file "NDJSON uploads export (header line + one object per upload)"
// @Failure      500 {object} map[string]string
// @Router       /admin/uploads/export [get]
// @Security     BearerAuth
// AdminExportUploads streams the upload catalog + DataMaps as an NDJSON download.
func AdminExportUploads(db *database.DB) http.HandlerFunc {
	backupSvc := services.NewBackupService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		callerID := middleware.GetUserID(r.Context())

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="indelible-uploads.ndjson"`)

		// Streaming starts here; once bytes are written the 200 status is fixed,
		// so a mid-stream error is captured in the audit detail rather than the
		// HTTP status. This export exposes every private DataMap → audit at warn.
		n, err := backupSvc.ExportUploads(w)
		detail := fmt.Sprintf("count=%d", n)
		if err != nil {
			detail = fmt.Sprintf("count=%d error=%v", n, err)
		}
		auditEvent(r, logSvc, "uploads_exported", "warn", &callerID, detail)
	}
}

// @Summary      Import uploads (catalog + DataMaps)
// @Description  Restore upload records from an NDJSON export produced by the export endpoint. Records are recreated with their original status, so completed uploads are immediately retrievable and the upload worker never re-uploads or re-pays. A uuid already present is skipped (idempotent restore). Owners are matched by email, falling back to the importing admin when unmatched.
// @Tags         Admin: Backup
// @Accept       application/x-ndjson
// @Produce      json
// @Success      200 {object} services.ImportResult
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/uploads/import [post]
// @Security     BearerAuth
// AdminImportUploads restores upload records from an NDJSON export.
func AdminImportUploads(db *database.DB) http.HandlerFunc {
	backupSvc := services.NewBackupService(db)
	logSvc := services.NewLogService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		importerID := middleware.GetUserID(r.Context())

		res, err := backupSvc.ImportUploads(r.Body, importerID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}

		auditEvent(r, logSvc, "uploads_imported", "warn", &importerID,
			fmt.Sprintf("imported=%d skipped=%d owner_fallback=%d errors=%d",
				res.Imported, res.Skipped, res.OwnerFallback, len(res.Errors)))

		jsonResponse(w, http.StatusOK, res)
	}
}
