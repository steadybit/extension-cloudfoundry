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

type restartAction struct{}

type RestartActionState struct {
	AppGUID string
	AppName string
}

var _ action_kit_sdk.Action[RestartActionState] = (*restartAction)(nil)

func NewRestartAction() action_kit_sdk.Action[RestartActionState] {
	return &restartAction{}
}

func (a *restartAction) NewEmptyState() RestartActionState {
	return RestartActionState{}
}

func (a *restartAction) Describe() action_kit_api.ActionDescription {
	return action_kit_api.ActionDescription{
		Id:          fmt.Sprintf("%s.restart", TargetType),
		Label:       "Restart App",
		Description: "Restart a Cloud Foundry application.",
		Version:     extbuild.GetSemverVersionStringOrUnknown(),
		Icon:        new(targetIcon),
		TargetSelection: new(action_kit_api.TargetSelection{
			TargetType: TargetType,
			SelectionTemplates: new([]action_kit_api.TargetSelectionTemplate{
				{
					Label:       "by app name",
					Description: new("Find app by name"),
					Query:       "cf.app.name=\"\"",
				},
				{
					Label:       "by space and app name",
					Description: new("Find app by space and name"),
					Query:       "cf.space.name=\"\" AND cf.app.name=\"\"",
				},
			}),
		}),
		Category:    new("resource"),
		Kind:        action_kit_api.Attack,
		TimeControl: action_kit_api.TimeControlInstantaneous,
		Parameters:  []action_kit_api.ActionParameter{},
	}
}

func (a *restartAction) Prepare(_ context.Context, state *RestartActionState, request action_kit_api.PrepareActionRequestBody) (*action_kit_api.PrepareResult, error) {
	appGUID := request.Target.Attributes["cf.app.guid"]
	if len(appGUID) == 0 {
		return nil, fmt.Errorf("target is missing cf.app.guid attribute")
	}
	appName := request.Target.Attributes["cf.app.name"]

	state.AppGUID = appGUID[0]
	if len(appName) > 0 {
		state.AppName = appName[0]
	}

	return nil, nil
}

func (a *restartAction) Start(_ context.Context, state *RestartActionState) (*action_kit_api.StartResult, error) {
	client := extclient.NewClient()

	log.Info().Str("appGUID", state.AppGUID).Str("appName", state.AppName).Msg("Restarting CF app")

	err := client.RestartApp(context.Background(), state.AppGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to restart app %s (%s): %w", state.AppName, state.AppGUID, err)
	}

	return &action_kit_api.StartResult{
		Messages: &[]action_kit_api.Message{
			{
				Level:   extutil.Ptr(action_kit_api.Info),
				Message: fmt.Sprintf("Restarted Cloud Foundry app '%s'", state.AppName),
			},
		},
	}, nil
}
