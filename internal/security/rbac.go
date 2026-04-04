package security

import (
	"net/http"
	"strings"
)

// RBACConfig configures role-based access control.
type RBACConfig struct {
	Enabled bool            `yaml:"enabled"`
	Roles   map[string]Role `yaml:"roles"`
}

// Role defines permissions for an RBAC role.
type Role struct {
	Name         string   `yaml:"name"`
	Permissions  []string `yaml:"permissions"`
	MaxRPM       int      `yaml:"max_rpm"`
	MaxBudget    float64  `yaml:"max_budget"`
	AllowedTiers []string `yaml:"allowed_tiers"`
}

// RBACEnforcer checks permissions against roles.
type RBACEnforcer struct {
	enabled bool
	roles   map[string]Role
}

// NewRBACEnforcer creates a new RBAC enforcer.
func NewRBACEnforcer(cfg RBACConfig) *RBACEnforcer {
	if !cfg.Enabled {
		return &RBACEnforcer{enabled: false}
	}

	// Add default roles if none defined
	if len(cfg.Roles) == 0 {
		cfg.Roles = map[string]Role{
			"admin": {
				Name:        "admin",
				Permissions: []string{"chat", "admin", "synonyms:read", "synonyms:write", "dashboard", "feedback"},
			},
			"user": {
				Name:        "user",
				Permissions: []string{"chat", "feedback"},
			},
			"viewer": {
				Name:        "viewer",
				Permissions: []string{"dashboard", "synonyms:read"},
			},
		}
	}

	return &RBACEnforcer{
		enabled: true,
		roles:   cfg.Roles,
	}
}

// HasPermission checks if a role has a specific permission.
func (e *RBACEnforcer) HasPermission(roleName, permission string) bool {
	if !e.enabled {
		return true
	}
	role, ok := e.roles[roleName]
	if !ok {
		return false
	}
	for _, p := range role.Permissions {
		if p == permission || p == "*" {
			return true
		}
		// Wildcard check: "synonyms:*" matches "synonyms:read"
		if strings.HasSuffix(p, ":*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(permission, prefix) {
				return true
			}
		}
	}
	return false
}

// GetRole returns the role configuration.
func (e *RBACEnforcer) GetRole(name string) (Role, bool) {
	if !e.enabled {
		return Role{Permissions: []string{"*"}}, true
	}
	role, ok := e.roles[name]
	return role, ok
}

// RequirePermission returns middleware that requires a specific permission.
func (e *RBACEnforcer) RequirePermission(permission string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !e.enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Get role from context (set by auth middleware)
			role, _ := r.Context().Value(ContextKeyRole).(string)
			if role == "" {
				role = "user"
			}

			if !e.HasPermission(role, permission) {
				http.Error(w, `{"error":"insufficient permissions","required":"`+permission+`"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// PathToPermission maps URL paths to required permissions.
func PathToPermission(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1/chat"):
		return "chat"
	case strings.HasPrefix(path, "/api/synonyms") && strings.Contains(path, "promote"):
		return "synonyms:write"
	case strings.HasPrefix(path, "/api/synonyms") && strings.Contains(path, "add"):
		return "synonyms:write"
	case strings.HasPrefix(path, "/api/synonyms"):
		return "synonyms:read"
	case strings.HasPrefix(path, "/dashboard"):
		return "dashboard"
	case strings.HasPrefix(path, "/v1/feedback"):
		return "feedback"
	case path == "/health" || path == "/metrics":
		return ""
	default:
		return "chat"
	}
}
