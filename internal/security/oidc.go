package security

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OIDCConfig configures OpenID Connect / SSO authentication.
type OIDCConfig struct {
	Enabled          bool     `yaml:"enabled"`
	Issuer           string   `yaml:"issuer"`
	ClientID         string   `yaml:"client_id"`
	ClientSecret     string   `yaml:"client_secret"`
	RedirectURL      string   `yaml:"redirect_url"`
	Scopes           []string `yaml:"scopes"`
	JWKSEndpoint     string   `yaml:"jwks_endpoint"`
	TokenEndpoint    string   `yaml:"token_endpoint"`
	AuthEndpoint     string   `yaml:"auth_endpoint"`
	UserInfoEndpoint string   `yaml:"userinfo_endpoint"`
	AllowedDomains   []string `yaml:"allowed_domains"`
}

// OIDCProvider handles OIDC/SSO authentication flows.
type OIDCProvider struct {
	config     OIDCConfig
	discovery  *OIDCDiscovery
	mu         sync.RWMutex
	httpClient *http.Client
}

// OIDCDiscovery contains the OpenID Connect discovery document.
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// OIDCClaims represents decoded JWT claims from an OIDC token.
type OIDCClaims struct {
	Subject  string   `json:"sub"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Groups   []string `json:"groups"`
	Domain   string   `json:"hd"`
	Issuer   string   `json:"iss"`
	Audience string   `json:"aud"`
	Expiry   int64    `json:"exp"`
	IssuedAt int64    `json:"iat"`
}

// NewOIDCProvider creates a new OIDC authentication provider.
func NewOIDCProvider(cfg OIDCConfig) (*OIDCProvider, error) {
	if !cfg.Enabled {
		return &OIDCProvider{config: cfg}, nil
	}

	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}

	provider := &OIDCProvider{
		config:     cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// Auto-discover endpoints if issuer is provided
	if cfg.Issuer != "" {
		discovery, err := provider.discover(cfg.Issuer)
		if err != nil {
			// Log warning but don't fail — endpoints may be manually configured
			fmt.Printf("[oidc] warning: auto-discovery failed: %v\n", err)
		} else {
			provider.discovery = discovery
			if cfg.JWKSEndpoint == "" {
				cfg.JWKSEndpoint = discovery.JWKSURI
			}
			if cfg.TokenEndpoint == "" {
				cfg.TokenEndpoint = discovery.TokenEndpoint
			}
			if cfg.AuthEndpoint == "" {
				cfg.AuthEndpoint = discovery.AuthorizationEndpoint
			}
			if cfg.UserInfoEndpoint == "" {
				cfg.UserInfoEndpoint = discovery.UserInfoEndpoint
			}
			provider.config = cfg
		}
	}

	return provider, nil
}

// discover fetches the OIDC discovery document.
func (p *OIDCProvider) discover(issuer string) (*OIDCDiscovery, error) {
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery returned %d: %s", resp.StatusCode, string(body))
	}

	var disc OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return nil, fmt.Errorf("failed to decode discovery: %w", err)
	}
	return &disc, nil
}

// ValidateToken validates a Bearer token by calling the UserInfo endpoint.
func (p *OIDCProvider) ValidateToken(token string) (*OIDCClaims, error) {
	if !p.config.Enabled || p.config.UserInfoEndpoint == "" {
		return nil, fmt.Errorf("OIDC not configured")
	}

	req, err := http.NewRequest("GET", p.config.UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token validation failed: status %d", resp.StatusCode)
	}

	var claims OIDCClaims
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}

	// Domain restriction
	if len(p.config.AllowedDomains) > 0 {
		domain := claims.Domain
		if domain == "" {
			parts := strings.Split(claims.Email, "@")
			if len(parts) == 2 {
				domain = parts[1]
			}
		}
		allowed := false
		for _, d := range p.config.AllowedDomains {
			if strings.EqualFold(domain, d) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("domain %q not in allowed list", domain)
		}
	}

	return &claims, nil
}

// Middleware returns HTTP middleware that validates OIDC Bearer tokens.
func (p *OIDCProvider) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !p.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip health/metrics endpoints
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token == auth {
				http.Error(w, `{"error":"invalid authorization format, expected Bearer token"}`, http.StatusUnauthorized)
				return
			}

			claims, err := p.ValidateToken(token)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"authentication failed: %s"}`, err.Error()), http.StatusUnauthorized)
				return
			}

			// Inject claims into context
			ctx := context.WithValue(r.Context(), ContextKeyTenant, claims.Email)
			ctx = context.WithValue(ctx, ContextKeyScopes, claims.Groups)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
