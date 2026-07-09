// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extapps

import (
	"context"
	"testing"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/extension-kit/extutil"
	"github.com/stretchr/testify/require"
)

func prepareRequest(config map[string]any) action_kit_api.PrepareActionRequestBody {
	config["duration"] = 1000 * 60
	config["expectedState"] = AppStateStarted
	config["stateCheckMode"] = stateCheckModeAllTheTime
	return extutil.JsonMangle(action_kit_api.PrepareActionRequestBody{
		Config: config,
		Target: new(action_kit_api.Target{
			Attributes: map[string][]string{
				"cloudfoundry.app.guid": {"guid-1"},
				"cloudfoundry.app.name": {"my-app"},
			},
		}),
	})
}

func TestCheckAppPrepareDefaultsFailEarlyTrue(t *testing.T) {
	// Given - failEarly not provided
	request := prepareRequest(map[string]any{})
	action := checkAppAction{}
	state := action.NewEmptyState()

	// When
	result, err := action.Prepare(context.TODO(), &state, request)

	// Then
	require.Nil(t, result)
	require.Nil(t, err)
	require.True(t, state.FailEarly) // defaults to true when not provided (non-breaking for old experiments)
}

func TestCheckAppPrepareExtractsFailEarlyFalse(t *testing.T) {
	// Given
	request := prepareRequest(map[string]any{"failEarly": false})
	action := checkAppAction{}
	state := action.NewEmptyState()

	// When
	result, err := action.Prepare(context.TODO(), &state, request)

	// Then
	require.Nil(t, result)
	require.Nil(t, err)
	require.False(t, state.FailEarly)
}
