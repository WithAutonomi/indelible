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

type adminUserResponse struct {
	ID               int64   `json:"id"`
	Email            string  `json:"email"`
	FirstName        string  `json:"first_name"`
	LastName         string  `json:"last_name"`
	IsActive         bool    `json:"is_active"`
	IsServiceAccount bool    `json:"is_service_account"`
	EmailVerified    bool    `json:"email_verified"`
	Permissions      string  `json:"permissions"`
	LastLoginAt      *string `json:"last_login_at"`
	CreatedAt        string  `json:"created_at"`
}

type adminListUsersResponse struct {
	Users []adminUserResponse `json:"users"`
	Total int64               `json:"total"`
	Limit int                 `json:"limit"`
	Offset int               `json:"offset"`
}

type createServiceAccountRequest struct {
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Permissions string `json:"permissions"` // read, write, admin
}

type setPermissionsRequest struct {
	Permission string `json:"permission"` // read, write, admin
}

type updateUserRequest struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	IsActive  *bool   `json:"is_active"`
}

func toAdminUserResponse(u *services.User, perms string) adminUserResponse {
	r := adminUserResponse{
		ID:               u.ID,
		Email:            u.Email,
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		IsActive:         u.IsActive,
		IsServiceAccount: u.IsServiceAccount,
		EmailVerified:    u.EmailVerified,
		Permissions:      perms,
		CreatedAt:        u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if u.LastLoginAt.Valid {
		t := u.LastLoginAt.Time.Format("2006-01-02T15:04:05Z")
		r.LastLoginAt = &t
	}
	return r
}

func AdminListUsers(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 || limit > 100 {
			limit = 50
		}

		users, total, err := userSvc.List(limit, offset)
		if err != nil {
			jsonError(w, "failed to list users", http.StatusInternalServerError)
			return
		}

		resp := adminListUsersResponse{
			Users:  make([]adminUserResponse, 0, len(users)),
			Total:  total,
			Limit:  limit,
			Offset: offset,
		}
		for _, u := range users {
			perms, _ := permSvc.GetEffective(u.ID)
			resp.Users = append(resp.Users, toAdminUserResponse(u, perms))
		}

		jsonResponse(w, http.StatusOK, resp)
	}
}

func AdminGetUser(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid user id", http.StatusBadRequest)
			return
		}

		user, err := userSvc.GetByID(id)
		if err != nil {
			if errors.Is(err, services.ErrUserNotFound) {
				jsonError(w, "user not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get user", http.StatusInternalServerError)
			return
		}

		perms, _ := permSvc.GetEffective(id)
		jsonResponse(w, http.StatusOK, toAdminUserResponse(user, perms))
	}
}

func AdminUpdateUser(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid user id", http.StatusBadRequest)
			return
		}

		var req updateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		firstName := ""
		if req.FirstName != nil {
			firstName = strings.TrimSpace(*req.FirstName)
		}
		lastName := ""
		if req.LastName != nil {
			lastName = strings.TrimSpace(*req.LastName)
		}

		if err := userSvc.Update(id, firstName, lastName, req.IsActive); err != nil {
			if errors.Is(err, services.ErrUserNotFound) {
				jsonError(w, "user not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to update user", http.StatusInternalServerError)
			return
		}

		user, _ := userSvc.GetByID(id)
		perms, _ := permSvc.GetEffective(id)
		jsonResponse(w, http.StatusOK, toAdminUserResponse(user, perms))
	}
}

func AdminDeleteUser(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid user id", http.StatusBadRequest)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		if id == callerID {
			jsonError(w, "cannot delete yourself", http.StatusBadRequest)
			return
		}

		if err := userSvc.SoftDelete(id); err != nil {
			jsonError(w, "failed to delete user", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{"message": "user deleted"})
	}
}

func AdminCreateServiceAccount(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req createServiceAccountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.FirstName == "" {
			jsonError(w, "email and first_name are required", http.StatusBadRequest)
			return
		}
		if req.Permissions == "" {
			req.Permissions = "read"
		}
		if req.Permissions != "read" && req.Permissions != "write" && req.Permissions != "admin" {
			jsonError(w, "permissions must be read, write, or admin", http.StatusBadRequest)
			return
		}

		// Create with no password (service accounts can't login)
		user, err := userSvc.CreateServiceAccount(req.Email, req.FirstName, req.LastName)
		if err != nil {
			if errors.Is(err, services.ErrEmailTaken) {
				jsonError(w, "email already registered", http.StatusConflict)
				return
			}
			jsonError(w, "failed to create service account", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		_ = permSvc.SetDirect(user.ID, req.Permissions, callerID)

		jsonResponse(w, http.StatusCreated, toAdminUserResponse(user, req.Permissions))
	}
}

func AdminSetPermissions(db *sql.DB) http.HandlerFunc {
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			jsonError(w, "invalid user id", http.StatusBadRequest)
			return
		}

		var req setPermissionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Permission != "read" && req.Permission != "write" && req.Permission != "admin" {
			jsonError(w, "permission must be read, write, or admin", http.StatusBadRequest)
			return
		}

		// Check we're not removing the last admin
		if req.Permission != "admin" {
			currentPerm, _ := permSvc.GetEffective(id)
			if currentPerm == "admin" {
				count, _ := permSvc.CountAdmins()
				if count <= 1 {
					jsonError(w, "cannot remove the last admin", http.StatusConflict)
					return
				}
			}
		}

		callerID := middleware.GetUserID(r.Context())
		if err := permSvc.SetDirect(id, req.Permission, callerID); err != nil {
			jsonError(w, "failed to set permissions", http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]string{
			"message":    "permissions updated",
			"permission": req.Permission,
		})
	}
}

