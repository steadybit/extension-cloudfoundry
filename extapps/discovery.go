// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package extapps

import (
	"context"
	"time"

	"github.com/steadybit/discovery-kit/go/discovery_kit_api"
	"github.com/steadybit/discovery-kit/go/discovery_kit_commons"
	"github.com/steadybit/discovery-kit/go/discovery_kit_sdk"
	"github.com/steadybit/extension-cloudfoundry/config"
	"github.com/steadybit/extension-cloudfoundry/extclient"
	"github.com/steadybit/extension-kit/extbuild"
)

type appDiscovery struct{}

var (
	_ discovery_kit_sdk.TargetDescriber    = (*appDiscovery)(nil)
	_ discovery_kit_sdk.AttributeDescriber = (*appDiscovery)(nil)
)

func NewAppDiscovery() discovery_kit_sdk.TargetDiscovery {
	discovery := &appDiscovery{}
	return discovery_kit_sdk.NewCachedTargetDiscovery(discovery,
		discovery_kit_sdk.WithRefreshTargetsNow(),
		discovery_kit_sdk.WithRefreshTargetsInterval(context.Background(), 60*time.Second),
	)
}

func (d *appDiscovery) Describe() discovery_kit_api.DiscoveryDescription {
	return discovery_kit_api.DiscoveryDescription{
		Id: TargetType,
		Discover: discovery_kit_api.DescribingEndpointReferenceWithCallInterval{
			CallInterval: new("60s"),
		},
	}
}

func (d *appDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       TargetType,
		Label:    discovery_kit_api.PluralLabel{One: "Cloud Foundry App", Other: "Cloud Foundry Apps"},
		Category: new("Cloud Foundry"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     new(targetIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "cloudfoundry.app.name"},
				{Attribute: "cloudfoundry.space.name"},
				{Attribute: "cloudfoundry.org.name"},
			},
			OrderBy: []discovery_kit_api.OrderBy{
				{Attribute: "cloudfoundry.app.name", Direction: "ASC"},
			},
		},
	}
}

func (d *appDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{
			Attribute: "cloudfoundry.app.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF App Name", Other: "CF App Names"},
		},
		{
			Attribute: "cloudfoundry.app.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF App GUID", Other: "CF App GUIDs"},
		},
		{
			Attribute: "cloudfoundry.space.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF Space Name", Other: "CF Space Names"},
		},
		{
			Attribute: "cloudfoundry.space.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF Space GUID", Other: "CF Space GUIDs"},
		},
		{
			Attribute: "cloudfoundry.org.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF Org Name", Other: "CF Org Names"},
		},
		{
			Attribute: "cloudfoundry.org.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF Org GUID", Other: "CF Org GUIDs"},
		},
		{
			Attribute: "cloudfoundry.app.lifecycle.type",
			Label:     discovery_kit_api.PluralLabel{One: "CF Lifecycle Type", Other: "CF Lifecycle Types"},
		},
		{
			Attribute: "cloudfoundry.app.reportedBy",
			Label:     discovery_kit_api.PluralLabel{One: "reported by", Other: "reported by"},
		},
	}
}

func (d *appDiscovery) DiscoverTargets(_ context.Context) ([]discovery_kit_api.Target, error) {
	client := extclient.NewClient()

	apps, spaces, orgs, err := client.ListApps(context.Background())
	if err != nil {
		return nil, err
	}

	// Build lookup maps
	spaceMap := make(map[string]extclient.Space)
	for _, s := range spaces {
		spaceMap[s.GUID] = s
	}
	orgMap := make(map[string]extclient.Organization)
	for _, o := range orgs {
		orgMap[o.GUID] = o
	}

	targets := make([]discovery_kit_api.Target, 0, len(apps))
	for _, app := range apps {
		attrs := map[string][]string{
			"cloudfoundry.app.name":           {app.Name},
			"cloudfoundry.app.guid":           {app.GUID},
			"cloudfoundry.app.lifecycle.type": {app.Lifecycle.Type},
			"cloudfoundry.app.reportedBy":     {"extension-cloudfoundry"},
		}

		spaceGUID := app.Relationships.Space.Data.GUID
		if spaceGUID != "" {
			attrs["cloudfoundry.space.guid"] = []string{spaceGUID}
			if space, ok := spaceMap[spaceGUID]; ok {
				attrs["cloudfoundry.space.name"] = []string{space.Name}
				orgGUID := space.Relationships.Organization.Data.GUID
				if orgGUID != "" {
					attrs["cloudfoundry.org.guid"] = []string{orgGUID}
					if org, ok := orgMap[orgGUID]; ok {
						attrs["cloudfoundry.org.name"] = []string{org.Name}
					}
				}
			}
		}

		targets = append(targets, discovery_kit_api.Target{
			Id:         app.GUID,
			TargetType: TargetType,
			Label:      app.Name,
			Attributes: attrs,
		})
	}

	return discovery_kit_commons.ApplyAttributeExcludes(targets, config.Config.DiscoveryAttributesExcludesApp), nil
}
