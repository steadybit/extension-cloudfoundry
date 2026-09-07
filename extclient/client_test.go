// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steadybit/extension-cloudfoundry/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cfMock is a minimal Cloud Foundry API plus UAA stand-in. Handlers are keyed by
// "METHOD /path"; a request without a handler gets a 404.
type cfMock struct {
	server        *httptest.Server
	handlers      map[string]http.HandlerFunc
	tokenRequests atomic.Int32
	// requests records "METHOD /path?query" of every request seen.
	mu       sync.Mutex
	requests []string
}

func newCfMock(t *testing.T) *cfMock {
	t.Helper()
	m := &cfMock{handlers: map[string]http.HandlerFunc{}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, r.Method+" "+r.URL.RequestURI())
		m.mu.Unlock()
		if h, ok := m.handlers[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(m.server.Close)

	// CF API root advertising this same server as the login endpoint.
	m.handle("GET /", func(w http.ResponseWriter, _ *http.Request) {
		writeJson(w, map[string]any{"links": map[string]any{"login": map[string]string{"href": m.server.URL}}})
	})
	// UAA password grant.
	m.handle("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		m.tokenRequests.Add(1)
		user, _, _ := r.BasicAuth()
		assert.Equal(t, "cf", user)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "password", r.Form.Get("grant_type"))
		assert.Equal(t, "alice", r.Form.Get("username"))
		assert.Equal(t, "s3cret", r.Form.Get("password"))
		writeJson(w, TokenResponse{AccessToken: "uaa-token", TokenType: "bearer", ExpiresIn: 3600})
	})
	return m
}

func (m *cfMock) handle(key string, h http.HandlerFunc) {
	m.handlers[key] = h
}

func (m *cfMock) handleStatus(key string, status int) {
	m.handle(key, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("nope"))
	})
}

func (m *cfMock) handleRaw(key, body string) {
	m.handle(key, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

// uaaClient returns a client that authenticates via UAA password grant.
func (m *cfMock) uaaClient() *Client {
	return &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, username: "alice", password: "s3cret"}
}

func writeJson(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func app(guid, space string) App {
	a := App{GUID: guid, Name: "app-" + guid, State: "STARTED"}
	a.Relationships.Space.Data.GUID = space
	return a
}

func space(guid, org string) Space {
	s := Space{GUID: guid, Name: "space-" + guid}
	s.Relationships.Organization.Data.GUID = org
	return s
}

func TestListApps_WithIncludesFollowsPaginationAndCachesTheToken(t *testing.T) {
	m := newCfMock(t)
	m.handle("GET /v3/apps", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bearer uaa-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Equal(t, "space,space.organization", r.URL.Query().Get("include"))

		var page ListAppsResponseWithIncludes
		if r.URL.Query().Get("page") == "2" {
			page.Resources = []App{app("a2", "s2")}
			page.Included.Spaces = []Space{space("s2", "o1")}
			page.Included.Organizations = []Organization{{GUID: "o1", Name: "org-1"}}
		} else {
			page.Resources = []App{app("a1", "s1")}
			page.Included.Spaces = []Space{space("s1", "o1")}
			page.Included.Organizations = []Organization{{GUID: "o1", Name: "org-1"}}
			page.Pagination.Next = &Link{Href: m.server.URL + "/v3/apps?page=2&include=space,space.organization"}
		}
		writeJson(w, page)
	})

	c := m.uaaClient()
	apps, spaces, orgs, err := c.ListApps(context.Background())
	require.NoError(t, err)
	assert.Len(t, apps, 2)
	assert.Len(t, spaces, 2)
	assert.Len(t, orgs, 1)

	// Second call must reuse the cached token instead of hitting UAA again.
	_, _, _, err = c.ListApps(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.tokenRequests.Load())
}

func TestListApps_FallsBackWithoutIncludesAndResolvesSpacesAndOrgs(t *testing.T) {
	m := newCfMock(t)
	m.handle("GET /v3/apps", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("include") {
			// Korifi rejects the include parameter.
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var page ListAppsResponse
		if r.URL.Query().Get("page") == "2" {
			page.Resources = []App{app("a3", "s-missing"), app("a4", "")}
		} else {
			page.Resources = []App{app("a1", "s1"), app("a2", "s1"), app("a5", "s2")}
			page.Pagination.Next = &Link{Href: m.server.URL + "/v3/apps?page=2"}
		}
		writeJson(w, page)
	})
	m.handle("GET /v3/spaces/s1", func(w http.ResponseWriter, _ *http.Request) { writeJson(w, space("s1", "o1")) })
	m.handle("GET /v3/spaces/s2", func(w http.ResponseWriter, _ *http.Request) { writeJson(w, space("s2", "o-missing")) })
	m.handleStatus("GET /v3/spaces/s-missing", http.StatusNotFound)
	m.handle("GET /v3/organizations/o1", func(w http.ResponseWriter, _ *http.Request) { writeJson(w, Organization{GUID: "o1", Name: "org-1"}) })
	m.handleStatus("GET /v3/organizations/o-missing", http.StatusNotFound)

	apps, spaces, orgs, err := m.uaaClient().ListApps(context.Background())
	require.NoError(t, err)
	assert.Len(t, apps, 5)
	assert.ElementsMatch(t, []Space{space("s1", "o1"), space("s2", "o-missing")}, spaces)
	assert.Equal(t, []Organization{{GUID: "o1", Name: "org-1"}}, orgs)
	// s1 is shared by two apps and must only be fetched once.
	assert.Equal(t, 1, countRequests(m, "GET /v3/spaces/s1"))
}

func TestListApps_FailsWhenBothVariantsFail(t *testing.T) {
	m := newCfMock(t)
	m.handleStatus("GET /v3/apps", http.StatusInternalServerError)

	_, _, _, err := m.uaaClient().ListApps(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list apps failed with status 500")
}

func TestListApps_FailsOnMalformedResponses(t *testing.T) {
	m := newCfMock(t)
	m.handleRaw("GET /v3/apps", "{not json")

	_, _, _, err := m.uaaClient().ListApps(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode list apps response")
}

func TestListApps_FailsWhenAuthenticationFails(t *testing.T) {
	m := newCfMock(t)
	m.handleStatus("POST /oauth/token", http.StatusUnauthorized)

	_, _, _, err := m.uaaClient().ListApps(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.Contains(t, err.Error(), "UAA token request failed with status 401")
}

func TestAuthenticate(t *testing.T) {
	t.Run("skips UAA for static token auth", func(t *testing.T) {
		m := newCfMock(t)
		c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, staticToken: "static"}
		require.NoError(t, c.authenticate(context.Background()))
		assert.Equal(t, "static", c.bearerToken())
		assert.Equal(t, 0, countRequests(m, ""))
	})

	t.Run("skips UAA for client certificate auth", func(t *testing.T) {
		m := newCfMock(t)
		c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, clientCertAuth: true}
		require.NoError(t, c.authenticate(context.Background()))
		assert.True(t, c.isClientCertAuth())
		assert.False(t, c.needsBearerAuth())
		assert.Equal(t, 0, countRequests(m, ""))
	})

	t.Run("falls back to the uaa link when there is no login link", func(t *testing.T) {
		m := newCfMock(t)
		m.handle("GET /", func(w http.ResponseWriter, _ *http.Request) {
			writeJson(w, map[string]any{"links": map[string]any{"uaa": map[string]string{"href": m.server.URL + "/uaa/"}}})
		})
		m.handle("POST /uaa/oauth/token", m.handlers["POST /oauth/token"])
		c := m.uaaClient()
		require.NoError(t, c.authenticate(context.Background()))
		assert.Equal(t, m.server.URL+"/uaa/", c.uaaEndpoint)
		assert.Equal(t, "uaa-token", c.bearerToken())
	})

	t.Run("fails when the root advertises no login endpoint", func(t *testing.T) {
		m := newCfMock(t)
		m.handleRaw("GET /", `{"links":{}}`)
		err := m.uaaClient().authenticate(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no UAA/login endpoint found")
	})

	t.Run("fails when the root is not json", func(t *testing.T) {
		m := newCfMock(t)
		m.handleRaw("GET /", "<html>")
		err := m.uaaClient().authenticate(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode CF API root response")
	})

	t.Run("fails when the root is unreachable", func(t *testing.T) {
		m := newCfMock(t)
		c := m.uaaClient()
		m.server.Close()
		err := c.authenticate(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to reach CF API root")
	})

	t.Run("fails when the token response is not json", func(t *testing.T) {
		m := newCfMock(t)
		m.handleRaw("POST /oauth/token", "<html>")
		err := m.uaaClient().authenticate(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode token response")
	})

	t.Run("re-authenticates once the token is about to expire", func(t *testing.T) {
		m := newCfMock(t)
		c := m.uaaClient()
		c.uaaEndpoint = m.server.URL
		c.accessToken = "old"
		c.tokenExpiry = time.Now().Add(30 * time.Second) // inside the 60s buffer
		require.NoError(t, c.authenticate(context.Background()))
		assert.Equal(t, "uaa-token", c.bearerToken())
		assert.Equal(t, int32(1), m.tokenRequests.Load())
	})
}

func TestDoAuthenticatedRequest_OmitsBearerHeaderForClientCertAuth(t *testing.T) {
	m := newCfMock(t)
	m.handle("GET /v3/apps/a1", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		writeJson(w, app("a1", "s1"))
	})
	c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, clientCertAuth: true}
	a, err := c.GetApp(context.Background(), "a1")
	require.NoError(t, err)
	assert.Equal(t, "app-a1", a.Name)
}

func TestDoAuthenticatedRequest_FailsOnInvalidUrl(t *testing.T) {
	c := &Client{httpClient: http.DefaultClient, staticToken: "static"}
	_, err := c.doAuthenticatedRequest(context.Background(), "GET", "http://[::1]:namedport")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

func TestGetApp(t *testing.T) {
	m := newCfMock(t)
	m.handle("GET /v3/apps/a1", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bearer static", r.Header.Get("Authorization"))
		writeJson(w, app("a1", "s1"))
	})
	m.handleStatus("GET /v3/apps/gone", http.StatusNotFound)
	m.handleRaw("GET /v3/apps/broken", "{")
	c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, staticToken: "static"}

	a, err := c.GetApp(context.Background(), "a1")
	require.NoError(t, err)
	assert.Equal(t, "a1", a.GUID)

	_, err = c.GetApp(context.Background(), "gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get app failed with status 404")

	_, err = c.GetApp(context.Background(), "broken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode get app response")

	m.server.Close()
	_, err = c.GetApp(context.Background(), "a1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get app")
}

func TestStopApp(t *testing.T) {
	m := newCfMock(t)
	m.handle("POST /v3/apps/a1/actions/stop", func(w http.ResponseWriter, _ *http.Request) {
		stopped := app("a1", "s1")
		stopped.State = "STOPPED"
		writeJson(w, stopped)
	})
	m.handleStatus("POST /v3/apps/gone/actions/stop", http.StatusNotFound)
	m.handleRaw("POST /v3/apps/broken/actions/stop", "{")
	c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, staticToken: "static"}

	a, err := c.StopApp(context.Background(), "a1")
	require.NoError(t, err)
	assert.Equal(t, "STOPPED", a.State)

	_, err = c.StopApp(context.Background(), "gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop app failed with status 404")

	_, err = c.StopApp(context.Background(), "broken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode stop app response")

	m.server.Close()
	_, err = c.StopApp(context.Background(), "a1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop app")
}

func TestRestartApp(t *testing.T) {
	m := newCfMock(t)
	m.handle("POST /v3/apps/a1/actions/restart", func(w http.ResponseWriter, _ *http.Request) { writeJson(w, app("a1", "s1")) })
	m.handleStatus("POST /v3/apps/gone/actions/restart", http.StatusNotFound)
	c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, staticToken: "static"}

	require.NoError(t, c.RestartApp(context.Background(), "a1"))

	err := c.RestartApp(context.Background(), "gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restart app failed with status 404")

	m.server.Close()
	err = c.RestartApp(context.Background(), "a1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restart app")
}

func TestGetSpaceAndGetOrg_FailOnMalformedResponses(t *testing.T) {
	m := newCfMock(t)
	m.handleRaw("GET /v3/spaces/s1", "{")
	m.handleRaw("GET /v3/organizations/o1", "{")
	c := &Client{httpClient: m.server.Client(), apiUrl: m.server.URL, staticToken: "static"}

	_, err := c.getSpace(context.Background(), "s1")
	require.Error(t, err)
	_, err = c.getOrg(context.Background(), "o1")
	require.Error(t, err)

	m.server.Close()
	_, err = c.getSpace(context.Background(), "s1")
	require.Error(t, err)
	_, err = c.getOrg(context.Background(), "o1")
	require.Error(t, err)
}

func TestNewClient_LoadsClientCertificate(t *testing.T) {
	certPath, keyPath := writeSelfSignedKeyPair(t)

	originalConfig := config.Config
	t.Cleanup(func() { config.Config = originalConfig })
	config.Config.ApiUrl = "https://api.cf.example.com/"
	config.Config.ClientCertPath = certPath
	config.Config.ClientKeyPath = keyPath
	config.Config.SkipTlsVerify = true

	c := NewClient()
	require.NotNil(t, c)
	assert.Same(t, c, NewClient(), "NewClient must return the singleton")
	assert.True(t, c.isClientCertAuth())
	assert.False(t, c.isStaticTokenAuth())
	assert.Equal(t, "https://api.cf.example.com", c.apiUrl)
	assert.True(t, c.skipTlsVerify)
	transport := c.httpClient.Transport.(*http.Transport)
	assert.Len(t, transport.TLSClientConfig.Certificates, 1)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func writeSelfSignedKeyPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDer, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0600))
	return certPath, keyPath
}

func countRequests(m *cfMock, prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}
