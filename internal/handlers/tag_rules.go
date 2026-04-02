package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
)

type tagRuleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	MatchField  string `json:"match_field"`
	MatchOp     string `json:"match_op"`
	MatchValue  string `json:"match_value"`
	ApplyKey    string `json:"apply_key"`
	ApplyValue  string `json:"apply_value"`
	Priority    int    `json:"priority"`
	IsEnabled   *bool  `json:"is_enabled"`
}

type tagRuleResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MatchField  string `json:"match_field"`
	MatchOp     string `json:"match_op"`
	MatchValue  string `json:"match_value"`
	ApplyKey    string `json:"apply_key"`
	ApplyValue  string `json:"apply_value"`
	Priority    int    `json:"priority"`
	IsEnabled   bool   `json:"is_enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toTagRuleResponse(r *services.TagRule) tagRuleResponse {
	return tagRuleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		MatchField:  r.MatchField,
		MatchOp:     r.MatchOp,
		MatchValue:  r.MatchValue,
		ApplyKey:    r.ApplyKey,
		ApplyValue:  r.ApplyValue,
		Priority:    r.Priority,
		IsEnabled:   r.IsEnabled,
		CreatedAt:   r.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   r.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ListTagRules returns all auto-tag rules.
func ListTagRules(db *sql.DB) http.HandlerFunc {
	svc := services.NewTagRuleService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		rules, err := svc.List()
		if err != nil {
			jsonError(w, "failed to list rules", http.StatusInternalServerError)
			return
		}
		resp := make([]tagRuleResponse, 0, len(rules))
		for _, r := range rules {
			resp = append(resp, toTagRuleResponse(r))
		}
		jsonResponse(w, http.StatusOK, map[string]any{"rules": resp})
	}
}

// CreateTagRule adds a new auto-tag rule.
func CreateTagRule(db *sql.DB) http.HandlerFunc {
	svc := services.NewTagRuleService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		var req tagRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.MatchField == "" || req.MatchOp == "" || req.ApplyKey == "" {
			jsonError(w, "name, match_field, match_op, and apply_key are required", http.StatusBadRequest)
			return
		}
		if req.Priority == 0 {
			req.Priority = 100
		}
		rule, err := svc.Create(req.Name, req.Description, req.MatchField, req.MatchOp, req.MatchValue, req.ApplyKey, req.ApplyValue, req.Priority, userID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonResponse(w, http.StatusCreated, map[string]any{"rule": toTagRuleResponse(rule)})
	}
}

// UpdateTagRule modifies an auto-tag rule.
func UpdateTagRule(db *sql.DB) http.HandlerFunc {
	svc := services.NewTagRuleService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid rule ID", http.StatusBadRequest)
			return
		}
		var req tagRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		isEnabled := true
		if req.IsEnabled != nil {
			isEnabled = *req.IsEnabled
		}
		rule, err := svc.Update(id, req.Name, req.Description, req.MatchField, req.MatchOp, req.MatchValue, req.ApplyKey, req.ApplyValue, req.Priority, isEnabled)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"rule": toTagRuleResponse(rule)})
	}
}

// DeleteTagRule removes an auto-tag rule.
func DeleteTagRule(db *sql.DB) http.HandlerFunc {
	svc := services.NewTagRuleService(db)
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid rule ID", http.StatusBadRequest)
			return
		}
		if err := svc.Delete(id); err != nil {
			jsonError(w, "failed to delete rule", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"message": "rule deleted"})
	}
}
