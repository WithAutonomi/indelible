package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/WithAutonomi/indelible/internal/auth"
	"github.com/WithAutonomi/indelible/internal/middleware"
	"github.com/WithAutonomi/indelible/internal/services"
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

type adminCreateUserRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Permissions string `json:"permissions"` // read, write, admin
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

// @Summary      List all users
// @Description  Return a paginated list of all users with their permissions
// @Tags         Admin: Users
// @Produce      json
// @Param        limit  query int false "Max results (default 50, max 100)"
// @Param        offset query int false "Offset for pagination"
// @Success      200 {object} adminListUsersResponse
// @Failure      500 {object} map[string]string
// @Router       /admin/users [get]
// @Security     BearerAuth
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

// @Summary      Get user by ID
// @Description  Return a single user's details and effective permissions
// @Tags         Admin: Users
// @Produce      json
// @Param        id path int true "User ID"
// @Success      200 {object} adminUserResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/users/{id} [get]
// @Security     BearerAuth
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

// @Summary      Update a user
// @Description  Update a user's first name, last name, or active status
// @Tags         Admin: Users
// @Accept       json
// @Produce      json
// @Param        id   path int              true "User ID"
// @Param        body body updateUserRequest true "Fields to update"
// @Success      200 {object} adminUserResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/users/{id} [put]
// @Security     BearerAuth
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

// @Summary      Delete a user
// @Description  Soft-delete a user account (cannot delete yourself)
// @Tags         Admin: Users
// @Produce      json
// @Param        id path int true "User ID"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/users/{id} [delete]
// @Security     BearerAuth
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

// @Summary      Create a user
// @Description  Create a new user account with email, password, name, and permissions
// @Tags         Admin: Users
// @Accept       json
// @Produce      json
// @Param        body body adminCreateUserRequest true "User details"
// @Success      201 {object} adminUserResponse
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/users [post]
// @Security     BearerAuth
func AdminCreateUser(db *sql.DB) http.HandlerFunc {
	userSvc := services.NewUserService(db)
	permSvc := services.NewPermissionService(db)

	return func(w http.ResponseWriter, r *http.Request) {
		var req adminCreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		req.FirstName = strings.TrimSpace(req.FirstName)
		req.LastName = strings.TrimSpace(req.LastName)

		if req.Email == "" || req.Password == "" || req.FirstName == "" || req.LastName == "" {
			jsonError(w, "email, password, first_name, and last_name are required", http.StatusBadRequest)
			return
		}
		if len(req.Password) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		if req.Permissions == "" {
			req.Permissions = "read"
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		user, err := userSvc.Create(req.Email, hash, req.FirstName, req.LastName)
		if err != nil {
			if errors.Is(err, services.ErrEmailTaken) {
				jsonError(w, "email already registered", http.StatusConflict)
				return
			}
			jsonError(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		callerID := middleware.GetUserID(r.Context())
		_ = permSvc.SetDirect(user.ID, req.Permissions, callerID)

		jsonResponse(w, http.StatusCreated, toAdminUserResponse(user, req.Permissions))
	}
}

// @Summary      Create a service account
// @Description  Create a non-login service account with specified permissions
// @Tags         Admin: Users
// @Accept       json
// @Produce      json
// @Param        body body createServiceAccountRequest true "Service account details"
// @Success      201 {object} adminUserResponse
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /admin/users/service-accounts [post]
// @Security     BearerAuth
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

// @Summary      Set user permissions
// @Description  Set the direct permission level for a user (read, write, or admin)
// @Tags         Admin: Users
// @Accept       json
// @Produce      json
// @Param        id   path int                  true "User ID"
// @Param        body body setPermissionsRequest true "Permission level"
// @Success      200 {object} map[string]string
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string "Cannot remove the last admin"
// @Failure      500 {object} map[string]string
// @Router       /admin/users/{id}/permissions [put]
// @Security     BearerAuth
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

