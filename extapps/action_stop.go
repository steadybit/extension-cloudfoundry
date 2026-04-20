// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extapps

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/extension-cloudfoundry/extclient"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/extutil"
)

type stopAction struct{}

type StopActionState struct {
	AppGUID      string
	AppName      string
	InitialState string
}

var _ action_kit_sdk.ActionWithStop[StopActionState] = (*stopAction)(nil)

func NewStopAction() action_kit_sdk.Action[StopActionState] {
	return &stopAction{}
}

func (a *stopAction) NewEmptyState() StopActionState {
	return StopActionState{}
}

func (a *stopAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.stop", TargetType),
		Label:       "Stop App",
		Description: "Stop a Cloud Foundry application. The app will be restarted when the action ends.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        new(targetIcon),
		TargetSelection: new(action_kit_api.TargetSelection{
			TargetType: TargetType,
			SelectionTemplates: new([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "by app name",
					Description: new("Find app by name"),
					Query:       "cloudfoundry.app.name=\"\"",
				},
				{
					Label:       "by space and app name",
					Description: new("Find app by space and name"),
					Query:       "cloudfoundry.space.name=\"\" AND cloudfoundry.app.name=\"\"",
				},
			}),
		}),
		Category:    new("Cloud Foundry"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlExternal,
		Parameters: []action_kit_api.ActionParameter{
			{
				Name:         "duration",
				Label:        "Duration",
				Description:  new("How long the app should be stopped."),
				Type:         action_kit_api.Duration,
				DefaultValue: new("60s"),
				Required:     new(true),
				Order:        new(0),
			},
		},
		Stop: new(action_kit_api.MutatingEndpointReference{}),
	}
}

func (a *stopAction) Prepare(_ context.Context, state *StopActionState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	appGUID := request.Target.Attributes["cloudfoundry.app.guid"]
	if len(appGUID) == 0 {
		return nil, fmt.Errorf("target is missing cloudfoundry.app.guid attribute")
	}
	appName := request.Target.Attributes["cloudfoundry.app.name"]

	state.AppGUID = appGUID[0]
	if len(appName) > 0 {
		state.AppName = appName[0]
	}

	// Fetch current state from the API to decide on rollback behavior
	client := extclient.NewClient()
	app, err := client.GetApp(context.Background(), state.AppGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current app state: %w", err)
	}
	state.InitialState = app.State

	return nil, nil
}

func (a *stopAction) Start(_ context.Context, state *StopActionState) (*action_kit_api.StartResult, error) {
	client := extclient.NewClient()

	log.Info().Str("appGUID", state.AppGUID).Str("appName", state.AppName).Msg("Stopping CF app")

	_, err := client.StopApp(context.Background(), state.AppGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to stop app %s (%s): %w", state.AppName, state.AppGUID, err)
	}

	return &action_kit_api.StartResult{
		Messages: &[]action_kit_api.Message{
			{
				Level:   extutil.Ptr(action_kit_api.Info),
				Message: fmt.Sprintf("Stopped Cloud Foundry app '%s'", state.AppName),
			},
		},
	}, nil
}

func (a *stopAction) Stop(_ context.Context, state *StopActionState) (*action_kit_api.StopResult, error) {
	// Only restart if the app was originally running
	if state.InitialState != AppStateStarted {
		log.Info().Str("appGUID", state.AppGUID).Str("appName", state.AppName).Str("initialState", state.InitialState).Msg("Skipping restart, app was not running before stop")
		return nil, nil
	}

	client := extclient.NewClient()

	log.Info().Str("appGUID", state.AppGUID).Str("appName", state.AppName).Msg("Restarting CF app (rollback)")

	err := client.RestartApp(context.Background(), state.AppGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to restart app %s (%s): %w", state.AppName, state.AppGUID, err)
	}

	return &action_kit_api.StopResult{
		Messages: &[]action_kit_api.Message{
			{
				Level:   extutil.Ptr(action_kit_api.Info),
				Message: fmt.Sprintf("Restarted Cloud Foundry app '%s'", state.AppName),
			},
		},
	}, nil
}
