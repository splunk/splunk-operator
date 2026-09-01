// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package indexercluster

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scaleOutStrategyFunc func(context.Context, int32, int32) (ScaleOutPlan, error)

func (f scaleOutStrategyFunc) NextReplicas(ctx context.Context, appliedReplicas, requestedReplicas int32) (ScaleOutPlan, error) {
	return f(ctx, appliedReplicas, requestedReplicas)
}

func TestPlanScaleOut(t *testing.T) {
	tests := []struct {
		name         string
		applied      int32
		requested    int32
		strategyPlan ScaleOutPlan
	}{
		{name: "one-at-a-time strategy", applied: 1, requested: 3, strategyPlan: ScaleOutPlan{TargetReplicas: 2}},
		{name: "unrestricted strategy", applied: 1, requested: 3, strategyPlan: ScaleOutPlan{TargetReplicas: 3}},
		{name: "batched strategy", applied: 2, requested: 7, strategyPlan: ScaleOutPlan{TargetReplicas: 5}},
		{name: "waits for currently applied peers", applied: 2, requested: 3, strategyPlan: ScaleOutPlan{TargetReplicas: 2}},
		{name: "waits for final convergence", applied: 3, requested: 3, strategyPlan: ScaleOutPlan{TargetReplicas: 3}},
		{name: "complete at requested count", applied: 3, requested: 3, strategyPlan: ScaleOutPlan{Complete: true, TargetReplicas: 3}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotApplied, gotRequested int32
			strategy := scaleOutStrategyFunc(func(_ context.Context, appliedReplicas, requestedReplicas int32) (ScaleOutPlan, error) {
				gotApplied = appliedReplicas
				gotRequested = requestedReplicas
				return test.strategyPlan, nil
			})

			plan, err := PlanScaleOut(t.Context(), strategy, test.applied, test.requested)
			require.NoError(t, err)
			assert.Equal(t, test.strategyPlan, plan)
			assert.Equal(t, test.applied, gotApplied)
			assert.Equal(t, test.requested, gotRequested)
		})
	}
}

func TestPlanScaleOutPropagatesStrategyError(t *testing.T) {
	wantErr := errors.New("peer observation failed")
	strategy := scaleOutStrategyFunc(func(context.Context, int32, int32) (ScaleOutPlan, error) {
		return ScaleOutPlan{}, wantErr
	})

	plan, err := PlanScaleOut(t.Context(), strategy, 1, 3)
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, int32(1), plan.TargetReplicas)
}

func TestPlanScaleOutRejectsScaleIn(t *testing.T) {
	called := false
	strategy := scaleOutStrategyFunc(func(context.Context, int32, int32) (ScaleOutPlan, error) {
		called = true
		return ScaleOutPlan{}, nil
	})

	plan, err := PlanScaleOut(t.Context(), strategy, 3, 2)
	require.ErrorContains(t, err, "cannot reduce replicas")
	assert.Equal(t, int32(3), plan.TargetReplicas)
	assert.False(t, called)
}

func TestPlanScaleOutRejectsUnsafeStrategyPlans(t *testing.T) {
	tests := []struct {
		name         string
		strategyPlan ScaleOutPlan
		wantError    string
	}{
		{name: "target below applied", strategyPlan: ScaleOutPlan{TargetReplicas: 1}, wantError: "cannot reduce replicas"},
		{name: "target above requested", strategyPlan: ScaleOutPlan{TargetReplicas: 4}, wantError: "exceeds requested replicas"},
		{name: "completion before requested count", strategyPlan: ScaleOutPlan{Complete: true, TargetReplicas: 2}, wantError: "cannot report completion"},
		{name: "completion while advancing", strategyPlan: ScaleOutPlan{Complete: true, TargetReplicas: 3}, wantError: "cannot report completion"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			strategy := scaleOutStrategyFunc(func(context.Context, int32, int32) (ScaleOutPlan, error) {
				return test.strategyPlan, nil
			})

			plan, err := PlanScaleOut(t.Context(), strategy, 2, 3)
			require.ErrorContains(t, err, test.wantError)
			assert.Equal(t, int32(2), plan.TargetReplicas)
		})
	}
}
