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
	"fmt"
)

// ScaleOutStrategy chooses the next safe replica target and reports when the
// requested scale-out is complete.
type ScaleOutStrategy interface {
	NextReplicas(context.Context, int32, int32) (ScaleOutPlan, error)
}

// ScaleOutPlan describes the safe replica target and whether the requested
// scale-out has converged.
type ScaleOutPlan struct {
	Complete       bool
	TargetReplicas int32
}

// PlanScaleOut delegates target selection to the strategy and validates that
// the result never authorizes scale-in, exceeds the request, or reports
// completion before the requested replica count has converged.
func PlanScaleOut(ctx context.Context, strategy ScaleOutStrategy, appliedReplicas, requestedReplicas int32) (ScaleOutPlan, error) {
	plan := ScaleOutPlan{TargetReplicas: appliedReplicas}
	if strategy == nil {
		return plan, fmt.Errorf("scale-out strategy is required")
	}
	if appliedReplicas < 0 {
		return plan, fmt.Errorf("applied replicas must not be negative: %d", appliedReplicas)
	}
	if requestedReplicas < 1 {
		return plan, fmt.Errorf("requested replicas must be at least one: %d", requestedReplicas)
	}
	if appliedReplicas > requestedReplicas {
		return plan, fmt.Errorf("scale-out workflow cannot reduce replicas from %d to %d", appliedReplicas, requestedReplicas)
	}

	strategyPlan, err := strategy.NextReplicas(ctx, appliedReplicas, requestedReplicas)
	if err != nil {
		return plan, err
	}
	if strategyPlan.TargetReplicas < appliedReplicas {
		return plan, fmt.Errorf("scale-out strategy cannot reduce replicas from %d to %d", appliedReplicas, strategyPlan.TargetReplicas)
	}
	if strategyPlan.TargetReplicas > requestedReplicas {
		return plan, fmt.Errorf("scale-out strategy target %d exceeds requested replicas %d", strategyPlan.TargetReplicas, requestedReplicas)
	}
	if strategyPlan.Complete && (appliedReplicas != requestedReplicas || strategyPlan.TargetReplicas != appliedReplicas) {
		return plan, fmt.Errorf("scale-out strategy cannot report completion at %d applied replicas with target %d when %d are requested", appliedReplicas, strategyPlan.TargetReplicas, requestedReplicas)
	}
	return strategyPlan, nil
}
