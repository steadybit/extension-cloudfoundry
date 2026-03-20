// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

type mockCfServer struct {
	http *httptest.Server
	mu   sync.Mutex
	apps map[string]*mockApp
}

type mockApp struct {
	GUID      string
	Name      string
	State     string
	SpaceGUID string
}

func createMockCfServer() *mockCfServer {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		panic(fmt.Sprintf("httptest: failed to listen: %v", err))
	}

	mock := &mockCfServer{
		apps: map[string]*mockApp{
			"app-guid-1": {
				GUID:      "app-guid-1",
				Name:      "my-web-app",
				State:     "STARTED",
				SpaceGUID: "space-guid-1",
			},
			"app-guid-2": {
				GUID:      "app-guid-2",
				Name:      "my-worker",
				State:     "STOPPED",
				SpaceGUID: "space-guid-1",
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/apps", mock.handleListApps)
	mux.HandleFunc("GET /v3/apps/{guid}", mock.handleGetApp)
	mux.HandleFunc("POST /v3/apps/{guid}/actions/stop", mock.handleStopApp)
	mux.HandleFunc("POST /v3/apps/{guid}/actions/restart", mock.handleRestartApp)
	mux.HandleFunc("GET /", mock.handleRoot)

	server := httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: mux},
	}
	server.Start()

	mock.http = &server
	log.Info().Str("url", server.URL).Msg("Started mock CF API server")
	return mock
}

func (m *mockCfServer) getAppState(guid string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if app, ok := m.apps[guid]; ok {
		return app.State
	}
	return ""
}

func (m *mockCfServer) setAppState(guid, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if app, ok := m.apps[guid]; ok {
		app.State = state
	}
}

func (m *mockCfServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Return a minimal CF API root response (no UAA since we use bearer token auth)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"links": map[string]interface{}{
			"self": map[string]string{"href": m.http.URL},
		},
	})
}

func (m *mockCfServer) handleListApps(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Debug().Msg("Mock CF: listing apps")

	resources := make([]map[string]interface{}, 0, len(m.apps))
	for _, app := range m.apps {
		resources = append(resources, m.appJSON(app))
	}

	resp := map[string]interface{}{
		"pagination": map[string]interface{}{
			"total_results": len(m.apps),
			"total_pages":   1,
			"next":          nil,
		},
		"resources": resources,
	}

	// If include=space,space.organization is requested, add included resources
	if strings.Contains(r.URL.RawQuery, "include=") {
		resp["included"] = map[string]interface{}{
			"spaces": []map[string]interface{}{
				{
					"guid": "space-guid-1",
					"name": "dev-space",
					"relationships": map[string]interface{}{
						"organization": map[string]interface{}{
							"data": map[string]string{"guid": "org-guid-1"},
						},
					},
				},
			},
			"organizations": []map[string]interface{}{
				{
					"guid": "org-guid-1",
					"name": "my-org",
				},
			},
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (m *mockCfServer) handleGetApp(w http.ResponseWriter, r *http.Request) {
	guid := r.PathValue("guid")

	m.mu.Lock()
	defer m.mu.Unlock()

	app, ok := m.apps[guid]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"errors": []map[string]string{{"detail": "App not found", "title": "CF-ResourceNotFound"}},
		})
		return
	}

	writeJSON(w, http.StatusOK, m.appJSON(app))
}

func (m *mockCfServer) handleStopApp(w http.ResponseWriter, r *http.Request) {
	guid := r.PathValue("guid")

	m.mu.Lock()
	defer m.mu.Unlock()

	app, ok := m.apps[guid]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"errors": []map[string]string{{"detail": "App not found", "title": "CF-ResourceNotFound"}},
		})
		return
	}

	app.State = "STOPPED"
	log.Info().Str("guid", guid).Str("name", app.Name).Msg("Mock CF: stopped app")
	writeJSON(w, http.StatusOK, m.appJSON(app))
}

func (m *mockCfServer) handleRestartApp(w http.ResponseWriter, r *http.Request) {
	guid := r.PathValue("guid")

	m.mu.Lock()
	defer m.mu.Unlock()

	app, ok := m.apps[guid]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"errors": []map[string]string{{"detail": "App not found", "title": "CF-ResourceNotFound"}},
		})
		return
	}

	app.State = "STARTED"
	log.Info().Str("guid", guid).Str("name", app.Name).Msg("Mock CF: restarted app")
	writeJSON(w, http.StatusOK, m.appJSON(app))
}

func (m *mockCfServer) appJSON(app *mockApp) map[string]interface{} {
	return map[string]interface{}{
		"guid":       app.GUID,
		"name":       app.Name,
		"state":      app.State,
		"created_at": "2025-01-01T00:00:00Z",
		"updated_at": "2025-01-01T00:00:00Z",
		"lifecycle": map[string]interface{}{
			"type": "buildpack",
			"data": map[string]interface{}{},
		},
		"relationships": map[string]interface{}{
			"space": map[string]interface{}{
				"data": map[string]string{"guid": app.SpaceGUID},
			},
		},
		"metadata": map[string]interface{}{
			"labels":      map[string]string{},
			"annotations": map[string]string{},
		},
		"links": map[string]interface{}{
			"self": map[string]string{"href": fmt.Sprintf("/v3/apps/%s", app.GUID)},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
