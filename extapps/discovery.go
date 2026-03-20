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
	"github.com/steadybit/extension-kit/extutil"
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
			CallInterval: extutil.Ptr("60s"),
		},
	}
}

func (d *appDiscovery) DescribeTarget() discovery_kit_api.TargetDescription {
	return discovery_kit_api.TargetDescription{
		Id:       TargetType,
		Label:    discovery_kit_api.PluralLabel{One: "Cloud Foundry App", Other: "Cloud Foundry Apps"},
		Category: extutil.Ptr("cloud"),
		Version:  extbuild.GetSemverVersionStringOrUnknown(),
		Icon:     extutil.Ptr(targetIcon),
		Table: discovery_kit_api.Table{
			Columns: []discovery_kit_api.Column{
				{Attribute: "cf.app.name"},
				{Attribute: "cf.space.name"},
				{Attribute: "cf.org.name"},
			},
			OrderBy: []discovery_kit_api.OrderBy{
				{Attribute: "cf.app.name", Direction: "ASC"},
			},
		},
	}
}

func (d *appDiscovery) DescribeAttributes() []discovery_kit_api.AttributeDescription {
	return []discovery_kit_api.AttributeDescription{
		{
			Attribute: "cf.app.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF App Name", Other: "CF App Names"},
		},
		{
			Attribute: "cf.app.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF App GUID", Other: "CF App GUIDs"},
		},
		{
			Attribute: "cf.space.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF Space Name", Other: "CF Space Names"},
		},
		{
			Attribute: "cf.space.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF Space GUID", Other: "CF Space GUIDs"},
		},
		{
			Attribute: "cf.org.name",
			Label:     discovery_kit_api.PluralLabel{One: "CF Org Name", Other: "CF Org Names"},
		},
		{
			Attribute: "cf.org.guid",
			Label:     discovery_kit_api.PluralLabel{One: "CF Org GUID", Other: "CF Org GUIDs"},
		},
		{
			Attribute: "cf.app.lifecycle.type",
			Label:     discovery_kit_api.PluralLabel{One: "CF Lifecycle Type", Other: "CF Lifecycle Types"},
		},
		{
			Attribute: "cf.app.reportedBy",
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
			"cf.app.name":           {app.Name},
			"cf.app.guid":           {app.GUID},
			"cf.app.lifecycle.type": {app.Lifecycle.Type},
			"cf.app.reportedBy":     {"extension-cloudfoundry"},
		}

		spaceGUID := app.Relationships.Space.Data.GUID
		if spaceGUID != "" {
			attrs["cf.space.guid"] = []string{spaceGUID}
			if space, ok := spaceMap[spaceGUID]; ok {
				attrs["cf.space.name"] = []string{space.Name}
				orgGUID := space.Relationships.Organization.Data.GUID
				if orgGUID != "" {
					attrs["cf.org.guid"] = []string{orgGUID}
					if org, ok := orgMap[orgGUID]; ok {
						attrs["cf.org.name"] = []string{org.Name}
					}
				}
			}
		}

		// Add labels as attributes
		for k, v := range app.Metadata.Labels {
			attrs["cf.app.label."+k] = []string{v}
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
