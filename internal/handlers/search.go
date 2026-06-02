package handlers

import (
	"net/http"
	"strings"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

// searchMinQuery is the shortest query we run; below it we return empty groups
// so a single keystroke doesn't scan every table.
const searchMinQuery = 2

// searchPerCategory caps hits per group — the omnibox shows a preview, not a
// full result page.
const searchPerCategory = 6

// GlobalSearch aggregates a LIKE search across entities for the omnibox (V2-406).
//
// Files, collections and tags are always searched and scoped to the calling
// user. The admin categories (users, tokens, webhooks) are searched only when
// the caller is an admin AND requested scope=all. That admin gate is enforced
// here server-side, so a non-admin cannot reach those categories by crafting
// the request — the endpoint itself lives in the authenticated (non-admin)
// route group.
//
// @Summary      Global search
// @Description  Search files/collections/tags (and, for admins with scope=all, users/tokens/webhooks)
// @Tags         Search
// @Produce      json
// @Param        q     query string false "Search query (min 2 chars)"
// @Param        scope query string false "Set to 'all' to include admin categories (admins only)"
// @Success      200 {object} map[string][]services.SearchHit
// @Security     BearerAuth
// @Router       /search [get]
func GlobalSearch(db *database.DB) http.HandlerFunc {
	searchSvc := services.NewSearchService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		q := strings.TrimSpace(r.URL.Query().Get("q"))

		result := map[string][]services.SearchHit{
			"files":       {},
			"collections": {},
			"tags":        {},
			"users":       {},
			"tokens":      {},
			"webhooks":    {},
		}

		if len([]rune(q)) < searchMinQuery {
			jsonResponse(w, http.StatusOK, result)
			return
		}

		// Caller-scoped categories. Best-effort: a failure in one category
		// leaves its group empty rather than failing the whole search.
		if hits, err := searchSvc.Files(userID, q, searchPerCategory); err == nil {
			result["files"] = hits
		}
		if hits, err := searchSvc.Collections(userID, q, searchPerCategory); err == nil {
			result["collections"] = hits
		}
		if hits, err := searchSvc.Tags(userID, q, searchPerCategory); err == nil {
			result["tags"] = hits
		}

		// Admin categories — only if the caller asked for them AND is an admin.
		if r.URL.Query().Get("scope") == "all" {
			if isAdmin, err := permSvc.IsAdmin(userID); err == nil && isAdmin {
				if hits, err := searchSvc.Users(q, searchPerCategory); err == nil {
					result["users"] = hits
				}
				if hits, err := searchSvc.Tokens(q, searchPerCategory); err == nil {
					result["tokens"] = hits
				}
				if hits, err := searchSvc.Webhooks(q, searchPerCategory); err == nil {
					result["webhooks"] = hits
				}
			}
		}

		jsonResponse(w, http.StatusOK, result)
	}
}
