// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/extension-cloudfoundry/config"
)

type Client struct {
	httpClient     *http.Client
	apiUrl         string
	username       string
	password       string
	staticToken    string
	clientCertAuth bool
	skipTlsVerify  bool

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
	uaaEndpoint string
}

// RootResponse represents the CF API root response used to discover the UAA endpoint.
type RootResponse struct {
	Links struct {
		Uaa struct {
			Href string `json:"href"`
		} `json:"uaa"`
		Login struct {
			Href string `json:"href"`
		} `json:"login"`
	} `json:"links"`
}

// TokenResponse represents the OAuth2 token response from UAA.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// App represents a Cloud Foundry application.
type App struct {
	GUID          string          `json:"guid"`
	Name          string          `json:"name"`
	State         string          `json:"state"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	Lifecycle     AppLifecycle    `json:"lifecycle"`
	Relationships AppRelations    `json:"relationships"`
	Metadata      AppMetadata     `json:"metadata"`
	Links         map[string]Link `json:"links"`
}

type AppLifecycle struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type AppRelations struct {
	Space struct {
		Data struct {
			GUID string `json:"guid"`
		} `json:"data"`
	} `json:"space"`
}

type AppMetadata struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type Link struct {
	Href string `json:"href"`
}

// ListAppsResponse represents the paginated response from GET /v3/apps.
type ListAppsResponse struct {
	Pagination struct {
		TotalResults int   `json:"total_results"`
		TotalPages   int   `json:"total_pages"`
		Next         *Link `json:"next"`
	} `json:"pagination"`
	Resources []App `json:"resources"`
}

// IncludedResources holds optionally included related resources.
type IncludedResources struct {
	Spaces        []Space        `json:"spaces,omitempty"`
	Organizations []Organization `json:"organizations,omitempty"`
}

type Space struct {
	GUID          string `json:"guid"`
	Name          string `json:"name"`
	Relationships struct {
		Organization struct {
			Data struct {
				GUID string `json:"guid"`
			} `json:"data"`
		} `json:"organization"`
	} `json:"relationships"`
}

type Organization struct {
	GUID string `json:"guid"`
	Name string `json:"name"`
}

type ListAppsResponseWithIncludes struct {
	ListAppsResponse
	Included IncludedResources `json:"included"`
}

var instance *Client
var once sync.Once

// NewClient creates a new CF API client from the global config.
func NewClient() *Client {
	once.Do(func() {
		transport := http.DefaultTransport.(*http.Transport).Clone()

		tlsConfig := &tls.Config{
			InsecureSkipVerify: config.Config.SkipTlsVerify, //nolint:gosec // user-configured
		}

		// Load client certificate for mTLS if configured
		clientCertAuth := false
		if config.Config.ClientCertPath != "" && config.Config.ClientKeyPath != "" {
			cert, err := tls.LoadX509KeyPair(config.Config.ClientCertPath, config.Config.ClientKeyPath)
			if err != nil {
				log.Fatal().Err(err).Msg("Failed to load client certificate")
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
			clientCertAuth = true
			log.Info().Msg("Using client certificate authentication")
		}

		transport.TLSClientConfig = tlsConfig

		instance = &Client{
			httpClient:     &http.Client{Timeout: 30 * time.Second, Transport: transport},
			apiUrl:         strings.TrimRight(config.Config.ApiUrl, "/"),
			username:       config.Config.Username,
			password:       config.Config.Password,
			staticToken:    config.Config.BearerToken,
			clientCertAuth: clientCertAuth,
			skipTlsVerify:  config.Config.SkipTlsVerify,
		}
	})
	return instance
}

func (c *Client) isStaticTokenAuth() bool {
	return c.staticToken != ""
}

func (c *Client) isClientCertAuth() bool {
	return c.clientCertAuth
}

func (c *Client) needsBearerAuth() bool {
	return !c.isClientCertAuth()
}

// discoverUaaEndpoint fetches the CF API root to find the UAA token endpoint.
func (c *Client) discoverUaaEndpoint(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create root request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach CF API root: %w", err)
	}
	defer resp.Body.Close()

	var root RootResponse
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return fmt.Errorf("failed to decode CF API root response: %w", err)
	}

	if root.Links.Login.Href != "" {
		c.uaaEndpoint = root.Links.Login.Href
	} else if root.Links.Uaa.Href != "" {
		c.uaaEndpoint = root.Links.Uaa.Href
	} else {
		return fmt.Errorf("no UAA/login endpoint found in CF API root response")
	}

	log.Debug().Str("uaaEndpoint", c.uaaEndpoint).Msg("Discovered UAA endpoint")
	return nil
}

// authenticate obtains a bearer token. For client cert or static token auth, it's a no-op.
// For UAA, it performs a password grant and caches the token.
func (c *Client) authenticate(ctx context.Context) error {
	if c.isClientCertAuth() || c.isStaticTokenAuth() {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached token if still valid (with 60s buffer)
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return nil
	}

	if c.uaaEndpoint == "" {
		if err := c.discoverUaaEndpoint(ctx); err != nil {
			return err
		}
	}

	tokenUrl := strings.TrimRight(c.uaaEndpoint, "/") + "/oauth/token"

	data := url.Values{
		"grant_type": {"password"},
		"username":   {c.username},
		"password":   {c.password},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// cf CLI client credentials (public client, no secret)
	req.SetBasicAuth("cf", "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UAA token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	log.Debug().Msg("Successfully authenticated with CF UAA")
	return nil
}

// bearerToken returns the current bearer token to use for requests.
func (c *Client) bearerToken() string {
	if c.isStaticTokenAuth() {
		return c.staticToken
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken
}

// doAuthenticatedRequest performs an HTTP request with the configured authentication.
// For client cert auth, TLS handles authentication. For bearer/UAA, an Authorization header is set.
func (c *Client) doAuthenticatedRequest(ctx context.Context, method, reqUrl string) (*http.Response, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.needsBearerAuth() {
		req.Header.Set("Authorization", "bearer "+c.bearerToken())
	}
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// ListApps fetches all applications from the CF API, following pagination.
// It tries to include space and org data via the include parameter. If the API
// doesn't support includes (e.g., Korifi), it falls back to fetching apps only
// and resolves spaces/orgs separately.
func (c *Client) ListApps(ctx context.Context) ([]App, []Space, []Organization, error) {
	// Try with includes first (standard CF API), fall back without (Korifi)
	apps, spaces, orgs, err := c.listAppsWithIncludes(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("ListApps with includes failed, retrying without includes")
		apps, err = c.listAppsSimple(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		spaces, orgs = c.resolveSpacesAndOrgs(ctx, apps)
	}

	log.Debug().Int("count", len(apps)).Int("spaces", len(spaces)).Int("orgs", len(orgs)).Msg("Listed CF apps")
	return apps, spaces, orgs, nil
}

func (c *Client) listAppsWithIncludes(ctx context.Context) ([]App, []Space, []Organization, error) {
	var allApps []App
	spacesMap := make(map[string]Space)
	orgsMap := make(map[string]Organization)

	nextUrl := c.apiUrl + "/v3/apps?per_page=200&include=space,space.organization"

	for nextUrl != "" {
		resp, err := c.doAuthenticatedRequest(ctx, http.MethodGet, nextUrl)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list apps: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, nil, nil, fmt.Errorf("list apps failed with status %d: %s", resp.StatusCode, string(body))
		}

		var result ListAppsResponseWithIncludes
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, nil, nil, fmt.Errorf("failed to decode list apps response: %w", err)
		}
		resp.Body.Close()

		allApps = append(allApps, result.Resources...)

		for _, s := range result.Included.Spaces {
			spacesMap[s.GUID] = s
		}
		for _, o := range result.Included.Organizations {
			orgsMap[o.GUID] = o
		}

		if result.Pagination.Next != nil {
			nextUrl = result.Pagination.Next.Href
		} else {
			nextUrl = ""
		}
	}

	spaces := make([]Space, 0, len(spacesMap))
	for _, s := range spacesMap {
		spaces = append(spaces, s)
	}
	orgs := make([]Organization, 0, len(orgsMap))
	for _, o := range orgsMap {
		orgs = append(orgs, o)
	}

	return allApps, spaces, orgs, nil
}

func (c *Client) listAppsSimple(ctx context.Context) ([]App, error) {
	var allApps []App
	nextUrl := c.apiUrl + "/v3/apps?per_page=200"

	for nextUrl != "" {
		resp, err := c.doAuthenticatedRequest(ctx, http.MethodGet, nextUrl)
		if err != nil {
			return nil, fmt.Errorf("failed to list apps: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("list apps failed with status %d: %s", resp.StatusCode, string(body))
		}

		var result ListAppsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode list apps response: %w", err)
		}
		resp.Body.Close()

		allApps = append(allApps, result.Resources...)

		if result.Pagination.Next != nil {
			nextUrl = result.Pagination.Next.Href
		} else {
			nextUrl = ""
		}
	}
	return allApps, nil
}

// resolveSpacesAndOrgs fetches space and org details for discovered apps.
func (c *Client) resolveSpacesAndOrgs(ctx context.Context, apps []App) ([]Space, []Organization) {
	spacesMap := make(map[string]Space)
	orgsMap := make(map[string]Organization)

	for _, app := range apps {
		spaceGUID := app.Relationships.Space.Data.GUID
		if spaceGUID == "" || spacesMap[spaceGUID].GUID != "" {
			continue
		}

		space, err := c.getSpace(ctx, spaceGUID)
		if err != nil {
			log.Debug().Err(err).Str("spaceGUID", spaceGUID).Msg("Failed to resolve space")
			continue
		}
		spacesMap[spaceGUID] = *space

		orgGUID := space.Relationships.Organization.Data.GUID
		if orgGUID == "" || orgsMap[orgGUID].GUID != "" {
			continue
		}

		org, err := c.getOrg(ctx, orgGUID)
		if err != nil {
			log.Debug().Err(err).Str("orgGUID", orgGUID).Msg("Failed to resolve org")
			continue
		}
		orgsMap[orgGUID] = *org
	}

	spaces := make([]Space, 0, len(spacesMap))
	for _, s := range spacesMap {
		spaces = append(spaces, s)
	}
	orgs := make([]Organization, 0, len(orgsMap))
	for _, o := range orgsMap {
		orgs = append(orgs, o)
	}
	return spaces, orgs
}

func (c *Client) getSpace(ctx context.Context, spaceGUID string) (*Space, error) {
	reqUrl := fmt.Sprintf("%s/v3/spaces/%s", c.apiUrl, spaceGUID)
	resp, err := c.doAuthenticatedRequest(ctx, http.MethodGet, reqUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get space failed with status %d: %s", resp.StatusCode, string(body))
	}

	var space Space
	if err := json.NewDecoder(resp.Body).Decode(&space); err != nil {
		return nil, err
	}
	return &space, nil
}

func (c *Client) getOrg(ctx context.Context, orgGUID string) (*Organization, error) {
	reqUrl := fmt.Sprintf("%s/v3/organizations/%s", c.apiUrl, orgGUID)
	resp, err := c.doAuthenticatedRequest(ctx, http.MethodGet, reqUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get org failed with status %d: %s", resp.StatusCode, string(body))
	}

	var org Organization
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return nil, err
	}
	return &org, nil
}

// StopApp stops an application by its GUID.
func (c *Client) StopApp(ctx context.Context, appGUID string) (*App, error) {
	reqUrl := fmt.Sprintf("%s/v3/apps/%s/actions/stop", c.apiUrl, appGUID)

	resp, err := c.doAuthenticatedRequest(ctx, http.MethodPost, reqUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to stop app: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("stop app failed with status %d: %s", resp.StatusCode, string(body))
	}

	var app App
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, fmt.Errorf("failed to decode stop app response: %w", err)
	}

	log.Info().Str("guid", appGUID).Str("name", app.Name).Msg("Stopped CF app")
	return &app, nil
}

// RestartApp restarts an application by its GUID.
func (c *Client) RestartApp(ctx context.Context, appGUID string) error {
	reqUrl := fmt.Sprintf("%s/v3/apps/%s/actions/restart", c.apiUrl, appGUID)

	resp, err := c.doAuthenticatedRequest(ctx, http.MethodPost, reqUrl)
	if err != nil {
		return fmt.Errorf("failed to restart app: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("restart app failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Info().Str("guid", appGUID).Msg("Restarted CF app")
	return nil
}

// GetApp fetches a single application by its GUID.
func (c *Client) GetApp(ctx context.Context, appGUID string) (*App, error) {
	reqUrl := fmt.Sprintf("%s/v3/apps/%s", c.apiUrl, appGUID)

	resp, err := c.doAuthenticatedRequest(ctx, http.MethodGet, reqUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get app failed with status %d: %s", resp.StatusCode, string(body))
	}

	var app App
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, fmt.Errorf("failed to decode get app response: %w", err)
	}

	return &app, nil
}
