package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type bulkTagRequest struct {
	UploadUUIDs []string          `json:"upload_uuids"`
	Selector    string            `json:"selector"`
	AddTags     map[string]string `json:"add_tags"`
	RemoveTags  []string          `json:"remove_tags"`
}

// BulkTagUploads applies or removes tags across multiple uploads.
// Targets can be specified by UUID list or by label selector.
func BulkTagUploads(db *sql.DB) http.HandlerFunc {
	uploadSvc := services.NewUploadService(db)
	tagSvc := services.NewTagService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		var req bulkTagRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.AddTags) == 0 && len(req.RemoveTags) == 0 {
			jsonError(w, "add_tags or remove_tags required", http.StatusBadRequest)
			return
		}

		// Resolve target uploads
		var uploadIDs []int64

		if len(req.UploadUUIDs) > 0 {
			// Resolve UUIDs to IDs, verifying ownership
			for _, uid := range req.UploadUUIDs {
				upload, err := uploadSvc.GetByUUID(uid)
				if err != nil {
					continue
				}
				if upload.UserID != userID {
					continue
				}
				uploadIDs = append(uploadIDs, upload.ID)
			}
		} else if req.Selector != "" {
			// Parse selector and find matching uploads
			reqs, err := services.ParseSelector(req.Selector)
			if err != nil {
				jsonError(w, "invalid selector: "+err.Error(), http.StatusBadRequest)
				return
			}
			clauses, args := services.BuildSelectorSQL(reqs)
			uploads, err := tagSvc.SearchBySelector(userID, clauses, args, 1000)
			if err != nil {
				jsonError(w, "search failed", http.StatusInternalServerError)
				return
			}
			for _, u := range uploads {
				uploadIDs = append(uploadIDs, u.ID)
			}
		} else {
			jsonError(w, "upload_uuids or selector required", http.StatusBadRequest)
			return
		}

		if len(uploadIDs) == 0 {
			jsonResponse(w, http.StatusOK, map[string]any{"affected": 0})
			return
		}

		// Apply tags
		affected := 0
		for _, id := range uploadIDs {
			if len(req.AddTags) > 0 {
				existing, _ := tagSvc.GetTags(id)
				merged := make(map[string]string)
				for k, v := range existing {
					merged[k] = v
				}
				for k, v := range req.AddTags {
					merged[k] = v
				}
				if err := tagSvc.SetTags(id, merged); err == nil {
					affected++
				}
			}
			if len(req.RemoveTags) > 0 {
				existing, _ := tagSvc.GetTags(id)
				for _, k := range req.RemoveTags {
					delete(existing, k)
				}
				tagSvc.SetTags(id, existing)
				if len(req.AddTags) == 0 {
					affected++
				}
			}
		}

		jsonResponse(w, http.StatusOK, map[string]any{"affected": affected})
	}
}

// TagFacets returns aggregated tag counts for the user's uploads.
func TagFacets(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		rows, err := db.Query(
			`SELECT ft.tag_key, ft.tag_value, COUNT(*) as count
			 FROM file_tags ft
			 INNER JOIN uploads u ON u.id = ft.upload_id
			 WHERE u.user_id = ?
			 GROUP BY ft.tag_key, ft.tag_value
			 ORDER BY ft.tag_key, count DESC`,
			userID,
		)
		if err != nil {
			jsonError(w, "failed to query tag facets", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type facetEntry struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Count int64  `json:"count"`
		}

		var facets []facetEntry
		for rows.Next() {
			var f facetEntry
			if err := rows.Scan(&f.Key, &f.Value, &f.Count); err != nil {
				continue
			}
			facets = append(facets, f)
		}

		if facets == nil {
			facets = []facetEntry{}
		}

		jsonResponse(w, http.StatusOK, map[string]any{"facets": facets})
	}
}
