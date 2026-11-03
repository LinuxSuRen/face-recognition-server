package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OIDCConfig configures the platform as an OIDC relying party (any
// standards-compliant issuer works, including home-gate).
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

// Configured reports whether federation login is on.
func (c OIDCConfig) Configured() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != ""
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// OIDCClient drives the authorization-code + PKCE flow.
type OIDCClient struct {
	Config OIDCConfig

	mu         sync.Mutex
	discovery  *oidcDiscovery
	discovered time.Time

	states map[string]pkceState
}

type pkceState struct {
	Verifier string
	Created  time.Time
}

// NewOIDCClient builds a client with an in-memory state store.
func NewOIDCClient(config OIDCConfig) *OIDCClient {
	if config.Scopes == "" {
		config.Scopes = "openid profile email"
	}
	return &OIDCClient{Config: config, states: map[string]pkceState{}}
}

func (c *OIDCClient) discover() (*oidcDiscovery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discovery != nil && time.Since(c.discovered) < time.Hour {
		return c.discovery, nil
	}
	issuer := strings.TrimRight(c.Config.Issuer, "/")
	response, err := http.Get(issuer + "/.well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("fetching OIDC discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery HTTP %d", response.StatusCode)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("decoding OIDC discovery: %w", err)
	}
	c.discovery = &discovery
	c.discovered = time.Now()
	return c.discovery, nil
}

// Discovery exposes the cached discovery document (tests).
func (c *OIDCClient) Discovery() (*oidcDiscovery, error) {
	return c.discover()
}

// StartURL builds the authorization redirect with PKCE; the state is
// remembered server-side for the callback.
func (c *OIDCClient) StartURL() (string, error) {
	discovery, err := c.discover()
	if err != nil {
		return "", err
	}
	state, verifier, err := newPKCE()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.states[state] = pkceState{Verifier: verifier, Created: time.Now()}
	// opportunistic cleanup
	for key, value := range c.states {
		if time.Since(value.Created) > 10*time.Minute {
			delete(c.states, key)
		}
	}
	c.mu.Unlock()

	challenge := base64.RawURLEncoding.EncodeToString(
		sum256(verifier))
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.Config.ClientID},
		"redirect_uri":          {c.Config.RedirectURL},
		"scope":                 {c.Config.Scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return discovery.AuthorizationEndpoint + "?" + values.Encode(), nil
}

// UserInfo exchanges the callback code and returns the identity claims.
type UserInfo struct {
	Subject  string
	Username string
	Name     string
}

func (c *OIDCClient) UserInfo(code, state string) (*UserInfo, error) {
	discovery, err := c.discover()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	stored, ok := c.states[state]
	delete(c.states, state)
	c.mu.Unlock()
	if !ok || time.Since(stored.Created) > 10*time.Minute {
		return nil, errors.New("unknown or expired login state")
	}

	tokenResponse, err := http.PostForm(discovery.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {c.Config.ClientID},
		"client_secret": {c.Config.ClientSecret},
		"redirect_uri":  {c.Config.RedirectURL},
		"code_verifier": {stored.Verifier},
	})
	if err != nil {
		return nil, fmt.Errorf("OIDC token exchange: %w", err)
	}
	defer tokenResponse.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(tokenResponse.Body, 1<<20))
	if tokenResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC token exchange HTTP %d: %s",
			tokenResponse.StatusCode, string(body))
	}
	var tokenPayload struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenPayload); err != nil {
		return nil, fmt.Errorf("decoding OIDC token: %w", err)
	}
	if tokenPayload.AccessToken == "" {
		return nil, errors.New("OIDC response carries no access token")
	}

	infoRequest, _ := http.NewRequest("GET", discovery.UserinfoEndpoint, nil)
	infoRequest.Header.Set("Authorization", "Bearer "+tokenPayload.AccessToken)
	infoResponse, err := http.DefaultClient.Do(infoRequest)
	if err != nil {
		return nil, fmt.Errorf("OIDC userinfo: %w", err)
	}
	defer infoResponse.Body.Close()
	if infoResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC userinfo HTTP %d", infoResponse.StatusCode)
	}
	var claims struct {
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		Email             string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(infoResponse.Body, 1<<20)).Decode(&claims); err != nil {
		return nil, fmt.Errorf("decoding OIDC userinfo: %w", err)
	}
	if claims.Sub == "" {
		return nil, errors.New("OIDC userinfo carries no subject")
	}
	username := claims.PreferredUsername
	if username == "" && claims.Email != "" {
		username = strings.SplitN(claims.Email, "@", 2)[0]
	}
	if username == "" {
		username = claims.Sub
	}
	return &UserInfo{Subject: claims.Sub, Username: username, Name: claims.Name}, nil
}

func newPKCE() (state, verifier string, err error) {
	stateBytes := make([]byte, 16)
	verifierBytes := make([]byte, 32)
	if _, err = rand.Read(stateBytes); err != nil {
		return
	}
	if _, err = rand.Read(verifierBytes); err != nil {
		return
	}
	state = base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier = base64.RawURLEncoding.EncodeToString(verifierBytes)
	return
}

func sum256(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}
