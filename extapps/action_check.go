// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extapps

import (
	"context"
	"fmt"
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
)

type checkAppAction struct{}

type CheckAppState struct {
	AppGUID           string
	AppName           string
	End               time.Time
	ExpectedState     string
	StateCheckMode    string
	StateCheckSuccess bool
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
				Name:         "expectedState",
				Label:        "Expected State",
				Description:  extutil.Ptr("The expected app state during the check."),
				Type:         action_kit_api.ActionParameterTypeString,
				DefaultValue: extutil.Ptr(AppStateStarted),
				Options: extutil.Ptr([]action_kit_api.ParameterOption{
					action_kit_api.ExplicitParameterOption{
						Label: "Started",
						Value: AppStateStarted,
					},
					action_kit_api.ExplicitParameterOption{
						Label: "Stopped",
						Value: AppStateStopped,
					},
				}),
				Required: extutil.Ptr(true),
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
	expectedState := fmt.Sprintf("%v", request.Config["expectedState"])

	var stateCheckMode string
	if request.Config["stateCheckMode"] != nil {
		stateCheckMode = fmt.Sprintf("%v", request.Config["stateCheckMode"])
	}

	state.AppGUID = appGUID[0]
	if len(appName) > 0 {
		state.AppName = appName[0]
	}
	state.End = time.Now().Add(time.Millisecond * time.Duration(duration))
	state.ExpectedState = expectedState
	state.StateCheckMode = stateCheckMode
	state.StateCheckSuccess = false

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
	completed := now.After(state.End)
	isExpected := currentState == state.ExpectedState

	var checkError *action_kit_api.ActionKitError

	if state.StateCheckMode == stateCheckModeAllTheTime {
		if !isExpected {
			checkError = extutil.Ptr(action_kit_api.ActionKitError{
				Title: fmt.Sprintf("App '%s' is in state '%s', expected '%s'.",
					state.AppName, currentState, state.ExpectedState),
				Status: extutil.Ptr(action_kit_api.Failed),
			})
		}
	} else if state.StateCheckMode == stateCheckModeAtLeastOnce {
		if isExpected {
			state.StateCheckSuccess = true
		}
		if completed && !state.StateCheckSuccess {
			checkError = extutil.Ptr(action_kit_api.ActionKitError{
				Title: fmt.Sprintf("App '%s' never reached state '%s' during check duration.",
					state.AppName, state.ExpectedState),
				Status: extutil.Ptr(action_kit_api.Failed),
			})
		}
	}

	metrics := []action_kit_api.Metric{
		*toAppStateMetric(state, currentState, now),
	}

	return &action_kit_api.StatusResult{
		Completed: completed,
		Error:     checkError,
		Metrics:   extutil.Ptr(metrics),
	}, nil
}

func toAppStateMetric(state *CheckAppState, currentState string, now time.Time) *action_kit_api.Metric {
	tooltip := fmt.Sprintf("State: %s", currentState)

	var metricState string
	if currentState == state.ExpectedState {
		metricState = "success"
	} else {
		metricState = "danger"
	}

	metricId := fmt.Sprintf("%s - Expected: %s", state.AppName, state.ExpectedState)

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
