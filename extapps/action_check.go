// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extapps

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-cloudfoundry/extclient"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

const (
	stateCheckModeAllTheTime  = "allTheTime"
	stateCheckModeAtLeastOnce = "atLeastOnce"

	expectedStateStarted  = "STARTED"
	expectedStateStopped  = "STOPPED"
	expectedStateNoEvents = "noEvents"
)

type checkAppAction struct{}

type CheckAppState struct {
	AppGUID           string
	AppName           string
	End               time.Time
	ExpectedStates    []string
	StateCheckMode    string
	StateCheckSuccess bool
	ObservedStates    map[string]bool // tracks which expected states have been seen
	PreviousState     string
}

var (
	_ action_kit_sdk.Action[CheckAppState]           = (*checkAppAction)(nil)
	_ action_kit_sdk.ActionWithStatus[CheckAppState] = (*checkAppAction)(nil)
)

func NewCheckAppAction() action_kit_sdk.Action[CheckAppState] {
	return &checkAppAction{}
}

func (a *checkAppAction) NewEmptyState() CheckAppState {
	return CheckAppState{}
}

func (a *checkAppAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.check", TargetType),
		Label:       "Check App State",
		Description: "Check the state of a Cloud Foundry application over time.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        extutil.Ptr(targetIcon),
		TargetSelection: extutil.Ptr(action_kit_api.TargetSelection{
			TargetType: TargetType,
			SelectionTemplates: extutil.Ptr([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "by app name",
					Description: extutil.Ptr("Find app by name"),
					Query:       "cf.app.name=\"\"",
				},
				{
					Label:       "by space and app name",
					Description: extutil.Ptr("Find app by space and name"),
					Query:       "cf.space.name=\"\" AND cf.app.name=\"\"",
				},
			}),
		}),
		Category:    extutil.Ptr("resource"),
		Kind:        action_kit_api.Check,
		TimeControl: action_kit_api.TimeControlInternal,
		Parameters: []action_kit_api.ActionParameter{
			{
				Name:         "duration",
				Label:        "Duration",
				Description:  extutil.Ptr("How long to check the app state."),
				Type:         action_kit_api.ActionParameterTypeDuration,
				DefaultValue: extutil.Ptr("30s"),
				Required:     extutil.Ptr(true),
				Order:        extutil.Ptr(0),
			},
			{
				Name:        "expectedStates",
				Label:       "Expected States",
				Description: extutil.Ptr("The expected app states during the check."),
				Type:        action_kit_api.ActionParameterTypeStringArray,
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{
						Label: "Started",
						Value: expectedStateStarted,
					},
					action_kit_api.ExplicitParameterOption{
						Label: "Stopped",
						Value: expectedStateStopped,
					},
					action_kit_api.ExplicitParameterOption{
						Label: "No events (no state change)",
						Value: expectedStateNoEvents,
					},
				}),
				Required: extutil.Ptr(false),
				Order:    extutil.Ptr(1),
			},
			{
				Name:         "stateCheckMode",
				Label:        "State Check Mode",
				Description:  extutil.Ptr("How should the state be checked?"),
				Type:         action_kit_api.ActionParameterTypeString,
				DefaultValue: extutil.Ptr(stateCheckModeAllTheTime),
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{
						Label: "All the time",
						Value: stateCheckModeAllTheTime,
					},
					action_kit_api.ExplicitParameterOption{
						Label: "At least once",
						Value: stateCheckModeAtLeastOnce,
					},
				}),
				Required: extutil.Ptr(true),
				Order:    extutil.Ptr(2),
			},
		},
		Widgets: extutil.Ptr([]action_kit_api.Widget{
			action_kit_api.StateOverTimeWidget{
				Type:  action_kit_api.ComSteadybitWidgetStateOverTime,
				Title: "CF App State",
				Identity: action_kit_api.StateOverTimeWidgetIdentityConfig{
					From: "metric.id",
				},
				Label: action_kit_api.StateOverTimeWidgetLabelConfig{
					From: "metric.id",
				},
				State: action_kit_api.StateOverTimeWidgetStateConfig{
					From: "state",
				},
				Tooltip: action_kit_api.StateOverTimeWidgetTooltipConfig{
					From: "tooltip",
				},
				Value: extutil.Ptr(action_kit_api.StateOverTimeWidgetValueConfig{
					Hide: extutil.Ptr(true),
				}),
			},
		}),
		Status: extutil.Ptr(action_kit_api.MutatingEndpointReferenceWithCallInterval{
			CallInterval: extutil.Ptr("500ms"),
		}),
	}
}

func (a *checkAppAction) Prepare(_ context.Context, state *CheckAppState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	appGUID := request.Target.Attributes["cf.app.guid"]
	if len(appGUID) == 0 {
		return nil, fmt.Errorf("target is missing cf.app.guid attribute")
	}
	appName := request.Target.Attributes["cf.app.name"]

	duration := request.Config["duration"].(float64)

	var expectedStates []string
	if request.Config["expectedStates"] != nil {
		expectedStates = extutil.ToStringArray(request.Config["expectedStates"])
	}

	var stateCheckMode string
	if request.Config["stateCheckMode"] != nil {
		stateCheckMode = fmt.Sprintf("%v", request.Config["stateCheckMode"])
	}

	// Get current app state as baseline
	client := extclient.NewClient()
	app, err := client.GetApp(context.Background(), appGUID[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get initial app state: %w", err)
	}

	state.AppGUID = appGUID[0]
	if len(appName) > 0 {
		state.AppName = appName[0]
	}
	state.End = time.Now().Add(time.Millisecond * time.Duration(duration))
	state.ExpectedStates = expectedStates
	state.StateCheckMode = stateCheckMode
	state.StateCheckSuccess = false
	state.ObservedStates = make(map[string]bool)
	state.PreviousState = app.State

	return nil, nil
}

func (a *checkAppAction) Start(_ context.Context, _ *CheckAppState) (*action_kit_api.StartResult, error) {
	return nil, nil
}

func (a *checkAppAction) Status(_ context.Context, state *CheckAppState) (*action_kit_api.StatusResult, error) {
	now := time.Now()

	client := extclient.NewClient()
	app, err := client.GetApp(context.Background(), state.AppGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app state: %w", err)
	}

	currentState := app.State
	stateChanged := currentState != state.PreviousState
	completed := now.After(state.End)

	// Track observed states
	if state.ObservedStates == nil {
		state.ObservedStates = make(map[string]bool)
	}
	state.ObservedStates[currentState] = true

	var checkError *action_kit_api.ActionKitError

	if len(state.ExpectedStates) > 0 {
		matchesExpected := matchesExpectedStates(state.ExpectedStates, currentState, stateChanged)

		if state.StateCheckMode == stateCheckModeAllTheTime {
			if !matchesExpected {
				checkError = extutil.Ptr(action_kit_api.ActionKitError{
					Title: fmt.Sprintf("App '%s' has unexpected state '%s' (changed: %v), expected: %v.",
						state.AppName, currentState, stateChanged, formatExpectedStates(state.ExpectedStates)),
					Status: extutil.Ptr(action_kit_api.Failed),
				})
			}
			// At completion, check that all non-noEvents expected states were observed
			if completed && checkError == nil {
				missing := missingExpectedStates(state.ExpectedStates, state.ObservedStates)
				if len(missing) > 0 {
					checkError = extutil.Ptr(action_kit_api.ActionKitError{
						Title: fmt.Sprintf("App '%s': expected states %v were never observed during check duration.",
							state.AppName, formatExpectedStates(missing)),
						Status: extutil.Ptr(action_kit_api.Failed),
					})
				}
			}
		} else if state.StateCheckMode == stateCheckModeAtLeastOnce {
			if matchesExpected {
				state.StateCheckSuccess = true
			}
			if completed && !state.StateCheckSuccess {
				checkError = extutil.Ptr(action_kit_api.ActionKitError{
					Title: fmt.Sprintf("App '%s' never reached expected state %v during check duration.",
						state.AppName, formatExpectedStates(state.ExpectedStates)),
					Status: extutil.Ptr(action_kit_api.Failed),
				})
			}
		}
	} else {
		// No expected states: check that nothing changes
		if stateChanged {
			checkError = extutil.Ptr(action_kit_api.ActionKitError{
				Title: fmt.Sprintf("App '%s' had unexpected state change from '%s' to '%s'.",
					state.AppName, state.PreviousState, currentState),
				Status: extutil.Ptr(action_kit_api.Failed),
			})
		}
	}

	// Update previous state for next poll
	state.PreviousState = currentState

	metrics := []action_kit_api.Metric{
		*toAppStateMetric(state, currentState, stateChanged, now),
	}

	return &action_kit_api.StatusResult{
		Completed: completed,
		Error:     checkError,
		Metrics:   extutil.Ptr(metrics),
	}, nil
}

// matchesExpectedStates checks if the current poll result matches the expected states.
func matchesExpectedStates(expectedStates []string, currentState string, stateChanged bool) bool {
	for _, expected := range expectedStates {
		switch expected {
		case expectedStateNoEvents:
			if !stateChanged {
				return true
			}
		case expectedStateStarted, expectedStateStopped:
			if currentState == expected {
				return true
			}
		}
	}
	return false
}

// missingExpectedStates returns expected states (excluding noEvents) that were never observed.
func missingExpectedStates(expectedStates []string, observedStates map[string]bool) []string {
	var missing []string
	for _, expected := range expectedStates {
		if expected == expectedStateNoEvents {
			continue
		}
		if !observedStates[expected] {
			missing = append(missing, expected)
		}
	}
	return missing
}

func formatExpectedStates(states []string) []string {
	formatted := make([]string, len(states))
	for i, s := range states {
		if s == expectedStateNoEvents {
			formatted[i] = "No events"
		} else {
			formatted[i] = s
		}
	}
	return formatted
}

func toAppStateMetric(state *CheckAppState, currentState string, stateChanged bool, now time.Time) *action_kit_api.Metric {
	tooltip := fmt.Sprintf("State: %s", currentState)

	// Use different widget states to visually distinguish app states:
	//   STARTED + expected   -> "success" (green/teal)
	//   STOPPED + expected   -> "warn"    (yellow/orange) — visually distinct from STARTED
	//   unexpected state     -> "danger"  (red)
	//   no change + expected -> "info"    (blue/grey)
	var metricState string
	isExpected := slices.Contains(state.ExpectedStates, currentState)
	noEventsExpected := slices.Contains(state.ExpectedStates, expectedStateNoEvents)

	onlyExpectsThis := len(state.ExpectedStates) == 1 && state.ExpectedStates[0] == currentState

	switch {
	case currentState == expectedStateStopped && isExpected && onlyExpectsThis:
		metricState = "success"
	case currentState == expectedStateStopped && isExpected:
		metricState = "warn"
	case currentState == expectedStateStopped && !isExpected:
		metricState = "danger"
	case currentState == expectedStateStarted && isExpected:
		metricState = "success"
	case currentState == expectedStateStarted && !isExpected && !noEventsExpected:
		metricState = "danger"
	case !stateChanged && (len(state.ExpectedStates) == 0 || noEventsExpected):
		metricState = "success"
	default:
		metricState = "info"
	}

	var metricId string
	if len(state.ExpectedStates) > 0 {
		metricId = fmt.Sprintf("%s - Expected: %v", state.AppName, formatExpectedStates(state.ExpectedStates))
	} else {
		metricId = fmt.Sprintf("%s - Expected: No changes", state.AppName)
	}

	return extutil.Ptr(action_kit_api.Metric{
		Name: extutil.Ptr("cf_app_state"),
		Metric: map[string]string{
			"metric.id": metricId,
			"state":     metricState,
			"tooltip":   tooltip,
		},
		Timestamp: now,
		Value:     0,
	})
}
