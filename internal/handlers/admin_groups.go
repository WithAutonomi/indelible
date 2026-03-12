package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maidsafe/indelible/internal/middleware"
	"github.com/maidsafe/indelible/internal/services"
)

type groupResponse struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permission_level"`
	IsActive        bool   `json:"is_active"`
	MemberCount     int64  `json:"member_count"`
	CreatedAt       string `json:"created_at"`
}

type createGroupRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permission_level"`
}

type updateGroupRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	PermissionLevel string `json:"permission_level"`
	IsActive        *bool  `json:"is_active"`
}

type addMemberRequest struct {
	UserID int64 `json:"user_id"`
}

func toGroupResponse(g *services.Group, memberCount int64) groupResponse {
	return groupResponse{
		ID:              g.ID,
		Name:            g.Name,
		Description:     g.Description,
		PermissionLevel: g.PermissionLevel,
		IsActive:        g.IsActive,
		MemberCount:     memberCount,
		CreatedAt:       g.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func validPermissionLevel(level string) bool {
	return level == "read" || level == "write" || level == "admin"
}

func AdminListGroups(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := groupSvc.List()
		if err != nil {
			jsonError(w, "failed to list groups", http.StatusInternalServerError)
			return
		}

		resp := make([]groupResponse, 0, len(groups))
		for _, g := range groups {
			count, _ := groupSvc.MemberCount(g.ID)
			resp = append(resp, toGroupResponse(g, count))
		}

		jsonResponse(w, http.StatusOK, map[string]any{"groups": resp})
	}
}

func AdminCreateGroup(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}
		if !validPermissionLevel(req.PermissionLevel) {
			jsonError(w, "permission_level must be read, write, or admin", http.StatusBadRequest)
			return
		}

		group, err := groupSvc.Create(req.Name, req.Description, req.PermissionLevel)
		if err != nil {
			if errors.Is(err, services.ErrGroupNameTaken) {
				jsonError(w, "group name already exists", http.StatusConflict)
				return
			}
			jsonError(w, "failed to create group", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, toGroupResponse(group, 0))
	}
}

func AdminUpdateGroup(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid group id", http.StatusBadRequest)
			return
		}

		var req updateGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.PermissionLevel != "" && !validPermissionLevel(req.PermissionLevel) {
			jsonError(w, "permission_level must be read, write, or admin", http.StatusBadRequest)
			return
		}

		if err := groupSvc.Update(id, strings.TrimSpace(req.Name), req.Description, req.PermissionLevel, req.IsActive); err != nil {
			if errors.Is(err, services.ErrGroupNotFound) {
				jsonError(w, "group not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update group", http.StatusInternalServerError)
			return
		}

		group, _ := groupSvc.GetByID(id)
		count, _ := groupSvc.MemberCount(id)
		jsonResponse(w, http.StatusOK, toGroupResponse(group, count))
	}
}

func AdminDeleteGroup(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid group id", http.StatusBadRequest)
			return
		}

		if err := groupSvc.Delete(id); err != nil {
			jsonError(w, "failed to delete group", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "group deleted"})
	}
}

func AdminAddGroupMember(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		groupID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid group id", http.StatusBadRequest)
			return
		}

		var req addMemberRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		addedBy := middleware.GetUserID(r.Context())
		if err := groupSvc.AddMember(groupID, req.UserID, addedBy); err != nil {
			if errors.Is(err, services.ErrAlreadyMember) {
				jsonError(w, "user is already a member", http.StatusConflict)
				return
			}
			jsonError(w, "failed to add member", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusCreated, map[string]string{"message": "member added"})
	}
}

func AdminRemoveGroupMember(db *sql.DB) http.HandlerFunc {
	groupSvc := services.NewGroupService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		groupID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid group id", http.StatusBadRequest)
			return
		}
		userID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
		if err != nil {
			jsonError(w, "invalid user id", http.StatusBadRequest)
			return
		}

		if err := groupSvc.RemoveMember(groupID, userID); err != nil {
			if errors.Is(err, services.ErrNotMember) {
				jsonError(w, "user is not a member", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to remove member", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "member removed"})
	}
}
