// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package main

import (
	"time"

	_ "github.com/KimMachineGun/automemlimit"
	"github.com/rs/zerolog"
	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/steadybit/action-kit/go/action_kit_sdk"
	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_sdk"
	"github.com/steadybit/extension-cloudfoundry/config"
	"github.com/steadybit/extension-cloudfoundry/extapps"
	"github.com/steadybit/extension-kit/extbuild"
	"github.com/steadybit/extension-kit/exthealth"
	"github.com/steadybit/extension-kit/exthttp"
	"github.com/steadybit/extension-kit/extlogging"
	"github.com/steadybit/extension-kit/extruntime"
	"github.com/steadybit/extension-kit/extsignals"
)

var startedAt = time.Now().Format(time.RFC3339)

func main() {
	extlogging.InitZeroLog()
	extbuild.PrintBuildInformation()
	extruntime.LogRuntimeInformation(zerolog.DebugLevel)

	config.ParseConfiguration()
	config.ValidateConfiguration()

	exthealth.SetReady(false)
	exthealth.StartProbes(8081)

	discovery_kit_sdk.Register(extapps.NewAppDiscovery())
	action_kit_sdk.RegisterAction(extapps.NewStopAction())
	action_kit_sdk.RegisterAction(extapps.NewRestartAction())
	action_kit_sdk.RegisterAction(extapps.NewCheckAppAction())

	extsignals.ActivateSignalHandlers()
	action_kit_sdk.RegisterCoverageEndpoints()

	exthttp.RegisterHttpHandler("/", exthttp.IfNoneMatchHandler(func() string { return startedAt }, exthttp.GetterAsHandler(getExtensionList)))

	exthealth.SetReady(true)

	exthttp.Listen(exthttp.ListenOpts{
		Port: 8080,
	})
}

type ExtensionListResponse struct {
	action_kit_api.ActionList       `json:",inline"`
	discovery_kit_api.DiscoveryList `json:",inline"`
}

func getExtensionList() ExtensionListResponse {
	return ExtensionListResponse{
		ActionList:    action_kit_sdk.GetActionList(),
		DiscoveryList: discovery_kit_sdk.GetDiscoveryList(),
	}
}
