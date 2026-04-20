// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_test/e2e"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_test/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithMinikube(t *testing.T) {
	server := createMockCfServer()
	defer server.http.Close()

	split := strings.SplitAfter(server.http.URL, ":")
	port := split[len(split)-1]

	extFactory := e2e.HelmExtensionFactory{
		Name: "extension-cloudfoundry",
		Port: 8080,
		ExtraArgs: func(m *e2e.Minikube) []string {
			return []string{
				"--set", "logging.level=debug",
				"--set", fmt.Sprintf("cloudfoundry.apiUrl=http://host.minikube.internal:%s", port),
				"--set", "cloudfoundry.bearerToken=mock-token",
			}
		},
	}

	e2e.WithDefaultMinikube(t, &extFactory, []e2e.WithMinikubeTestCase{
		{
			Name: "validate discovery",
			Test: validateDiscovery,
		},
		{
			Name: "target discovery",
			Test: testDiscovery,
		},
		{
			Name: "stop action",
			Test: testStopAction(server),
		},
		{
			Name: "restart action",
			Test: testRestartAction(server),
		},
		{
			Name: "check app state - started all the time - success",
			Test: testCheckStartedAllTheTimeSuccess(server),
		},
		{
			Name: "check app state - started all the time - failure on stop",
			Test: testCheckStartedAllTheTimeFailure(server),
		},
		{
			Name: "check app state - stopped at least once - success",
			Test: testCheckStoppedAtLeastOnceSuccess(server),
		},
	})
}

func validateDiscovery(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	assert.NoError(t, validate.ValidateEndpointReferences("/", e.Client))
}

func testDiscovery(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	target, err := e2e.PollForTarget(ctx, e, "com.steadybit.extension_cloudfoundry.app", func(target discovery_kit_api.Target) bool {
		return e2e.HasAttribute(target, "cloudfoundry.app.name", "my-web-app")
	})

	require.NoError(t, err)
	assert.Equal(t, "com.steadybit.extension_cloudfoundry.app", target.TargetType)
	assert.Equal(t, []string{"app-guid-1"}, target.Attributes["cloudfoundry.app.guid"])
	assert.Equal(t, []string{"extension-cloudfoundry"}, target.Attributes["cloudfoundry.app.reportedBy"])
	assert.Equal(t, []string{"dev-space"}, target.Attributes["cloudfoundry.space.name"])
	assert.Equal(t, []string{"my-org"}, target.Attributes["cloudfoundry.org.name"])
}

func testStopAction(server *mockCfServer) func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		config := struct {
			Duration int `json:"duration"`
		}{Duration: 5000}

		target := &action_kit_api.Target{
			Name: "my-web-app",
			Attributes: map[string][]string{
				"cloudfoundry.app.guid": {"app-guid-1"},
				"cloudfoundry.app.name": {"my-web-app"},
			},
		}

		exec, err := e.RunAction("com.steadybit.extension_cloudfoundry.app.stop", target, config, nil)
		require.NoError(t, err)

		// App should have been stopped
		assert.Equal(t, "STOPPED", server.getAppState("app-guid-1"))

		require.NoError(t, exec.Cancel())

		// After rollback, app should be started again
		assert.Equal(t, "STARTED", server.getAppState("app-guid-1"))
	}
}

func testRestartAction(server *mockCfServer) func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		target := &action_kit_api.Target{
			Name: "my-web-app",
			Attributes: map[string][]string{
				"cloudfoundry.app.guid": {"app-guid-1"},
				"cloudfoundry.app.name": {"my-web-app"},
			},
		}

		exec, err := e.RunAction("com.steadybit.extension_cloudfoundry.app.restart", target, nil, nil)
		require.NoError(t, err)
		require.NoError(t, exec.Cancel())

		// App should still be running
		assert.Equal(t, "STARTED", server.getAppState("app-guid-1"))
	}
}

func checkTarget() *action_kit_api.Target {
	return &action_kit_api.Target{
		Name: "my-web-app",
		Attributes: map[string][]string{
			"cloudfoundry.app.guid": {"app-guid-1"},
			"cloudfoundry.app.name": {"my-web-app"},
		},
	}
}

// STARTED + "All the time": should succeed when app stays started.
func testCheckStartedAllTheTimeSuccess(server *mockCfServer) func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		server.setAppState("app-guid-1", "STARTED")

		config := struct {
			Duration       int    `json:"duration"`
			ExpectedState  string `json:"expectedState"`
			StateCheckMode string `json:"stateCheckMode"`
		}{
			Duration:       5000,
			ExpectedState:  "STARTED",
			StateCheckMode: "allTheTime",
		}

		exec, err := e.RunAction("com.steadybit.extension_cloudfoundry.app.check", checkTarget(), config, nil)
		require.NoError(t, err)
		require.NoError(t, exec.Cancel())
	}
}

// STARTED + "All the time": should fail when app gets stopped during check.
func testCheckStartedAllTheTimeFailure(server *mockCfServer) func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		server.setAppState("app-guid-1", "STARTED")

		config := struct {
			Duration       int    `json:"duration"`
			ExpectedState  string `json:"expectedState"`
			StateCheckMode string `json:"stateCheckMode"`
		}{
			Duration:       10000,
			ExpectedState:  "STARTED",
			StateCheckMode: "allTheTime",
		}

		exec, err := e.RunAction("com.steadybit.extension_cloudfoundry.app.check", checkTarget(), config, nil)
		require.NoError(t, err)

		// Stop app during the check to trigger failure
		time.Sleep(3 * time.Second)
		server.setAppState("app-guid-1", "STOPPED")

		err = exec.Wait()
		require.Error(t, err)
		require.ErrorContains(t, err, "STOPPED")

		// Reset for subsequent tests
		server.setAppState("app-guid-1", "STARTED")
	}
}

// STOPPED + "At least once": should succeed when app gets stopped during check.
func testCheckStoppedAtLeastOnceSuccess(server *mockCfServer) func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
	return func(t *testing.T, _ *e2e.Minikube, e *e2e.Extension) {
		server.setAppState("app-guid-1", "STARTED")

		config := struct {
			Duration       int    `json:"duration"`
			ExpectedState  string `json:"expectedState"`
			StateCheckMode string `json:"stateCheckMode"`
		}{
			Duration:       8000,
			ExpectedState:  "STOPPED",
			StateCheckMode: "atLeastOnce",
		}

		exec, err := e.RunAction("com.steadybit.extension_cloudfoundry.app.check", checkTarget(), config, nil)
		require.NoError(t, err)

		// Stop app mid-check — should succeed because we see STOPPED at least once
		time.Sleep(3 * time.Second)
		server.setAppState("app-guid-1", "STOPPED")

		require.NoError(t, exec.Cancel())

		// Reset
		server.setAppState("app-guid-1", "STARTED")
	}
}
