package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/elimity-com/scim"
	scimerrors "github.com/elimity-com/scim/errors"
	"github.com/elimity-com/scim/optional"
	"github.com/elimity-com/scim/schema"
	filter "github.com/scim2/filter-parser/v2"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

// NewSCIMServer creates a configured SCIM 2.0 server as an http.Handler.
func NewSCIMServer(db *database.DB) (http.Handler, error) {
	userHandler := &scimUserHandler{
		userSvc: services.NewUserService(db),
		permSvc: services.NewPermissionService(db),
		logSvc:  services.NewLogService(db),
	}
	groupHandler := &scimGroupHandler{
		groupSvc: services.NewGroupService(db),
		logSvc:   services.NewLogService(db),
	}

	server, err := scim.NewServer(&scim.ServerArgs{
		ServiceProviderConfig: &scim.ServiceProviderConfig{
			SupportPatch:     true,
			SupportFiltering: true,
			MaxResults:       100,
			AuthenticationSchemes: []scim.AuthenticationScheme{
				{
					Type:        scim.AuthenticationTypeOauthBearerToken,
					Name:        "OAuth Bearer Token",
					Description: "Authentication scheme using the OAuth Bearer Token Standard",
				},
			},
		},
		ResourceTypes: []scim.ResourceType{
			{
				ID:          optional.NewString("User"),
				Name:        "User",
				Endpoint:    "/Users",
				Description: optional.NewString("User Account"),
				Schema:      schema.CoreUserSchema(),
				Handler:     userHandler,
			},
			{
				ID:          optional.NewString("Group"),
				Name:        "Group",
				Endpoint:    "/Groups",
				Description: optional.NewString("Group"),
				Schema:      schema.CoreGroupSchema(),
				Handler:     groupHandler,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return server, nil
}

// --- User Resource Handler ---

type scimUserHandler struct {
	userSvc *services.UserService
	permSvc *services.PermissionService
	logSvc  *services.LogService
}

func (h *scimUserHandler) Create(r *http.Request, attributes scim.ResourceAttributes) (scim.Resource, error) {
	email, firstName, lastName, externalID, _ := scimAttrsToUser(attributes)

	user, err := h.userSvc.CreateFromSCIM(email, firstName, lastName, externalID)
	if err != nil {
		return scim.Resource{}, err
	}

	// Set default read permission
	_ = h.permSvc.SetDirect(user.ID, "read", 0)

	h.audit(r, "scim.user.create", fmt.Sprintf("created user %s (id=%d)", email, user.ID))
	return userToSCIMResource(user), nil
}

func (h *scimUserHandler) Get(r *http.Request, id string) (scim.Resource, error) {
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}
	user, err := h.userSvc.GetByID(uid)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}
	return userToSCIMResource(user), nil
}

func (h *scimUserHandler) GetAll(r *http.Request, params scim.ListRequestParams) (scim.Page, error) {
	// Check for userName eq "..." filter — optimize with GetByEmail
	if params.FilterValidator != nil {
		f := params.FilterValidator.GetFilter()
		if attrExpr, ok := f.(*filter.AttributeExpression); ok {
			if attrExpr.AttributePath.String() == "userName" && attrExpr.Operator == filter.EQ {
				emailFilter := attrExpr.CompareValue.(string)
				user, err := h.userSvc.GetByEmail(emailFilter)
				if err != nil {
					return scim.Page{TotalResults: 0, Resources: []scim.Resource{}}, nil
				}
				return scim.Page{
					TotalResults: 1,
					Resources:    []scim.Resource{userToSCIMResource(user)},
				}, nil
			}
		}
	}

	// Default: paginated list
	offset := 0
	if params.StartIndex > 0 {
		offset = params.StartIndex - 1
	}
	count := params.Count
	if count <= 0 {
		count = 100
	}

	users, total, err := h.userSvc.List(count, offset)
	if err != nil {
		return scim.Page{}, err
	}

	resources := make([]scim.Resource, 0, len(users))
	for _, u := range users {
		resources = append(resources, userToSCIMResource(u))
	}

	return scim.Page{
		TotalResults: int(total),
		Resources:    resources,
	}, nil
}

func (h *scimUserHandler) Replace(r *http.Request, id string, attributes scim.ResourceAttributes) (scim.Resource, error) {
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}

	email, firstName, lastName, externalID, isActive := scimAttrsToUser(attributes)

	var extPtr *string
	if externalID != "" {
		extPtr = &externalID
	}

	if err := h.userSvc.UpdateFromSCIM(uid, email, firstName, lastName, extPtr, isActive); err != nil {
		return scim.Resource{}, err
	}

	user, err := h.userSvc.GetByID(uid)
	if err != nil {
		return scim.Resource{}, err
	}

	h.audit(r, "scim.user.replace", fmt.Sprintf("replaced user %d", uid))
	return userToSCIMResource(user), nil
}

func (h *scimUserHandler) Delete(r *http.Request, id string) error {
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scimerrors.ScimErrorResourceNotFound(id)
	}
	if err := h.userSvc.SoftDelete(uid); err != nil {
		return err
	}
	h.audit(r, "scim.user.delete", fmt.Sprintf("soft-deleted user %d", uid))
	return nil
}

func (h *scimUserHandler) Patch(r *http.Request, id string, operations []scim.PatchOperation) (scim.Resource, error) {
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}

	user, err := h.userSvc.GetByID(uid)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}

	firstName := user.FirstName
	lastName := user.LastName
	active := user.IsActive
	email := user.Email
	extID := ""
	if user.ExternalID.Valid {
		extID = user.ExternalID.String
	}

	for _, op := range operations {
		path := ""
		if op.Path != nil {
			path = op.Path.String()
		}

		switch op.Op {
		case "replace", "add":
			switch path {
			case "active":
				if v, ok := op.Value.(bool); ok {
					active = v
				}
			case "name.givenName":
				if v, ok := op.Value.(string); ok {
					firstName = v
				}
			case "name.familyName":
				if v, ok := op.Value.(string); ok {
					lastName = v
				}
			case "userName":
				if v, ok := op.Value.(string); ok {
					email = v
				}
			case "externalId":
				if v, ok := op.Value.(string); ok {
					extID = v
				}
			case "":
				// Bulk attribute update (no path)
				if m, ok := op.Value.(map[string]interface{}); ok {
					applyUserAttrsFromMap(m, &email, &firstName, &lastName, &extID, &active)
				}
			}
		}
	}

	extPtr := &extID
	if err := h.userSvc.UpdateFromSCIM(uid, email, firstName, lastName, extPtr, &active); err != nil {
		return scim.Resource{}, err
	}

	user, err = h.userSvc.GetByID(uid)
	if err != nil {
		return scim.Resource{}, err
	}

	h.audit(r, "scim.user.patch", fmt.Sprintf("patched user %d", uid))
	return userToSCIMResource(user), nil
}

func (h *scimUserHandler) audit(r *http.Request, eventType, detail string) {
	_ = h.logSvc.WriteAudit(eventType, "info", nil, detail, r.RemoteAddr, r.UserAgent())
}

// --- Group Resource Handler ---

type scimGroupHandler struct {
	groupSvc *services.GroupService
	logSvc   *services.LogService
}

func (h *scimGroupHandler) Create(r *http.Request, attributes scim.ResourceAttributes) (scim.Resource, error) {
	displayName, externalID, memberIDs := scimAttrsToGroup(attributes)

	group, err := h.groupSvc.Create(displayName, "", "read")
	if err != nil {
		return scim.Resource{}, err
	}

	if externalID != "" {
		_ = h.groupSvc.SetExternalID(group.ID, externalID)
	}

	for _, uid := range memberIDs {
		_ = h.groupSvc.AddMember(group.ID, uid, 0)
	}

	h.audit(r, "scim.group.create", fmt.Sprintf("created group %s (id=%d)", displayName, group.ID))
	return h.groupToResource(group)
}

func (h *scimGroupHandler) Get(r *http.Request, id string) (scim.Resource, error) {
	gid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}
	group, err := h.groupSvc.GetByID(gid)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}
	return h.groupToResource(group)
}

func (h *scimGroupHandler) GetAll(r *http.Request, params scim.ListRequestParams) (scim.Page, error) {
	groups, err := h.groupSvc.List()
	if err != nil {
		return scim.Page{}, err
	}

	// Apply pagination
	total := len(groups)
	start := 0
	if params.StartIndex > 0 {
		start = params.StartIndex - 1
	}
	if start > total {
		start = total
	}
	end := start + params.Count
	if end > total || params.Count <= 0 {
		end = total
	}

	resources := make([]scim.Resource, 0, end-start)
	for _, g := range groups[start:end] {
		res, err := h.groupToResource(g)
		if err != nil {
			continue
		}
		resources = append(resources, res)
	}

	return scim.Page{
		TotalResults: total,
		Resources:    resources,
	}, nil
}

func (h *scimGroupHandler) Replace(r *http.Request, id string, attributes scim.ResourceAttributes) (scim.Resource, error) {
	gid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}

	displayName, externalID, memberIDs := scimAttrsToGroup(attributes)

	if err := h.groupSvc.Update(gid, displayName, "", "", nil); err != nil {
		return scim.Resource{}, err
	}

	if externalID != "" {
		_ = h.groupSvc.SetExternalID(gid, externalID)
	}

	// Replace members atomically
	if err := h.groupSvc.ReplaceMembers(gid, memberIDs, 0); err != nil {
		return scim.Resource{}, err
	}

	group, err := h.groupSvc.GetByID(gid)
	if err != nil {
		return scim.Resource{}, err
	}

	h.audit(r, "scim.group.replace", fmt.Sprintf("replaced group %d", gid))
	return h.groupToResource(group)
}

func (h *scimGroupHandler) Delete(r *http.Request, id string) error {
	gid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scimerrors.ScimErrorResourceNotFound(id)
	}
	if err := h.groupSvc.Delete(gid); err != nil {
		return err
	}
	h.audit(r, "scim.group.delete", fmt.Sprintf("deleted group %d", gid))
	return nil
}

func (h *scimGroupHandler) Patch(r *http.Request, id string, operations []scim.PatchOperation) (scim.Resource, error) {
	gid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return scim.Resource{}, scimerrors.ScimErrorResourceNotFound(id)
	}

	for _, op := range operations {
		path := ""
		if op.Path != nil {
			path = op.Path.String()
		}

		switch op.Op {
		case "replace":
			if path == "displayName" {
				if v, ok := op.Value.(string); ok {
					_ = h.groupSvc.Update(gid, v, "", "", nil)
				}
			}
		case "add":
			if path == "members" {
				for _, uid := range parseMemberValues(op.Value) {
					_ = h.groupSvc.AddMember(gid, uid, 0)
				}
			}
		case "remove":
			if path == "members" {
				for _, uid := range parseMemberValues(op.Value) {
					_ = h.groupSvc.RemoveMember(gid, uid)
				}
			}
			// Handle members[value eq "123"] path filter
			if op.Path != nil {
				pathStr := op.Path.String()
				if uid := parseMemberPathFilter(pathStr); uid > 0 {
					_ = h.groupSvc.RemoveMember(gid, uid)
				}
			}
		}
	}

	group, err := h.groupSvc.GetByID(gid)
	if err != nil {
		return scim.Resource{}, err
	}

	h.audit(r, "scim.group.patch", fmt.Sprintf("patched group %d", gid))
	return h.groupToResource(group)
}

func (h *scimGroupHandler) groupToResource(g *services.Group) (scim.Resource, error) {
	memberIDs, _ := h.groupSvc.ListMembers(g.ID)
	members := make([]interface{}, 0, len(memberIDs))
	for _, uid := range memberIDs {
		members = append(members, map[string]interface{}{
			"value":   strconv.FormatInt(uid, 10),
			"$ref":    fmt.Sprintf("Users/%d", uid),
			"display": "",
		})
	}

	attrs := scim.ResourceAttributes{
		"displayName": g.Name,
		"members":     members,
	}

	res := scim.Resource{
		ID:         strconv.FormatInt(g.ID, 10),
		Attributes: attrs,
		Meta: scim.Meta{
			Created:      &g.CreatedAt,
			LastModified: &g.UpdatedAt,
		},
	}
	if g.ExternalID.Valid {
		res.ExternalID = optional.NewString(g.ExternalID.String)
	}
	return res, nil
}

func (h *scimGroupHandler) audit(r *http.Request, eventType, detail string) {
	_ = h.logSvc.WriteAudit(eventType, "info", nil, detail, r.RemoteAddr, r.UserAgent())
}

// --- Helper functions ---

func userToSCIMResource(u *services.User) scim.Resource {
	created := u.CreatedAt
	updated := u.UpdatedAt

	attrs := scim.ResourceAttributes{
		"userName": u.Email,
		"name": map[string]interface{}{
			"givenName":  u.FirstName,
			"familyName": u.LastName,
		},
		"active": u.IsActive,
		"emails": []interface{}{
			map[string]interface{}{
				"value":   u.Email,
				"primary": true,
			},
		},
	}

	res := scim.Resource{
		ID:         strconv.FormatInt(u.ID, 10),
		Attributes: attrs,
		Meta: scim.Meta{
			Created:      &created,
			LastModified: &updated,
		},
	}
	if u.ExternalID.Valid {
		res.ExternalID = optional.NewString(u.ExternalID.String)
	}
	return res
}

func scimAttrsToUser(attrs scim.ResourceAttributes) (email, firstName, lastName, externalID string, isActive *bool) {
	if v, ok := attrs["userName"].(string); ok {
		email = v
	}
	if nameMap, ok := attrs["name"].(map[string]interface{}); ok {
		if v, ok := nameMap["givenName"].(string); ok {
			firstName = v
		}
		if v, ok := nameMap["familyName"].(string); ok {
			lastName = v
		}
	}
	if v, ok := attrs["externalId"].(string); ok {
		externalID = v
	}
	if v, ok := attrs["active"].(bool); ok {
		isActive = &v
	}
	return
}

func scimAttrsToGroup(attrs scim.ResourceAttributes) (displayName, externalID string, memberIDs []int64) {
	if v, ok := attrs["displayName"].(string); ok {
		displayName = v
	}
	if v, ok := attrs["externalId"].(string); ok {
		externalID = v
	}
	if members, ok := attrs["members"].([]interface{}); ok {
		for _, m := range members {
			if memberMap, ok := m.(map[string]interface{}); ok {
				if val, ok := memberMap["value"].(string); ok {
					if uid, err := strconv.ParseInt(val, 10, 64); err == nil {
						memberIDs = append(memberIDs, uid)
					}
				}
			}
		}
	}
	return
}

func applyUserAttrsFromMap(m map[string]interface{}, email, firstName, lastName, extID *string, active *bool) {
	if v, ok := m["userName"].(string); ok {
		*email = v
	}
	if nameMap, ok := m["name"].(map[string]interface{}); ok {
		if v, ok := nameMap["givenName"].(string); ok {
			*firstName = v
		}
		if v, ok := nameMap["familyName"].(string); ok {
			*lastName = v
		}
	}
	if v, ok := m["externalId"].(string); ok {
		*extID = v
	}
	if v, ok := m["active"].(bool); ok {
		*active = v
	}
}

func parseMemberValues(value interface{}) []int64 {
	var ids []int64
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if val, ok := m["value"].(string); ok {
					if uid, err := strconv.ParseInt(val, 10, 64); err == nil {
						ids = append(ids, uid)
					}
				}
			}
		}
	case map[string]interface{}:
		if val, ok := v["value"].(string); ok {
			if uid, err := strconv.ParseInt(val, 10, 64); err == nil {
				ids = append(ids, uid)
			}
		}
	}
	return ids
}

// parseMemberPathFilter extracts user ID from path like `members[value eq "123"]`.
func parseMemberPathFilter(pathStr string) int64 {
	prefix := `value eq "`
	idx := 0
	for i := range pathStr {
		if i+len(prefix) <= len(pathStr) && pathStr[i:i+len(prefix)] == prefix {
			idx = i + len(prefix)
			break
		}
	}
	if idx == 0 {
		return 0
	}
	end := idx
	for end < len(pathStr) && pathStr[end] != '"' {
		end++
	}
	if uid, err := strconv.ParseInt(pathStr[idx:end], 10, 64); err == nil {
		return uid
	}
	return 0
}
