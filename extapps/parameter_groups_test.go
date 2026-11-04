// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2026 Steadybit GmbH

package extapps

import (
	"testing"

	"github.com/steadybit/action-kit/go/action_kit_api/v2"
	"github.com/stretchr/testify/assert"
)

func parameterOrder(p action_kit_api.ActionParameter) int {
	if p.Order == nil {
		return 0
	}
	return *p.Order
}

func TestParameterGroupsOfCheckAppAction(t *testing.T) {
	description := NewCheckAppAction().Describe()

	var failEarly *action_kit_api.ActionParameter
	maxUngroupedOrder := 0
	for i, p := range description.Parameters {
		if p.Advanced != nil && *p.Advanced {
			assert.NotNil(t, p.Group, "advanced parameter %q must have a group", p.Name)
		}
		if p.Name == "failEarly" {
			failEarly = &description.Parameters[i]
		}
		if p.Group == nil && parameterOrder(p) > maxUngroupedOrder {
			maxUngroupedOrder = parameterOrder(p)
		}
	}

	if assert.NotNil(t, failEarly) {
		assert.False(t, failEarly.Advanced != nil && *failEarly.Advanced, "failEarly must not be advanced")
		assert.Nil(t, failEarly.Group, "failEarly must not be grouped")
	}

	for _, p := range description.Parameters {
		if p.Group != nil && *p.Group == action_kit_api.ParameterGroupAdditionalSettings {
			assert.Greater(t, parameterOrder(p), maxUngroupedOrder, "%q must sort after all other parameters", p.Name)
		}
	}
}
