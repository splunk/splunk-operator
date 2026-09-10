// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

// noahDependencyReconcileOutcome keeps shared dependency semantics consistent
// while workload reconcilers still live in the legacy enterprise package.
type noahDependencyReconcileOutcome struct {
	phase           enterpriseApi.Phase
	message         string
	conditionReason enterpriseApi.ConditionReason
	conditionStatus metav1.ConditionStatus
	err             error
}

func newNoahDependencyResolvedCondition(status metav1.ConditionStatus, reason enterpriseApi.ConditionReason, message string) metav1.Condition {
	return metav1.Condition{
		Type:    string(enterpriseApi.ConditionNoahDependencyResolved),
		Status:  status,
		Reason:  string(reason),
		Message: message,
	}
}

// resolveNoahDependency resolves the referenced NoahCluster and its
// authentication Secret, records the outcome as a condition, and returns the
// runtime so callers can reuse it instead of resolving again. A nil runtime
// means the dependency is unusable and the caller must not proceed.
func resolveNoahDependency(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, conditions *[]metav1.Condition, ref *corev1.LocalObjectReference) (*configworkflow.NoahRuntime, error) {
	if ref == nil || ref.Name == "" {
		return nil, fmt.Errorf("noahClusterRef.name must not be empty")
	}

	runtime, err := configworkflow.ResolveNoahRuntime(ctx, client, cr.GetNamespace(), *ref)
	if err == nil {
		condition := newNoahDependencyResolvedCondition(
			metav1.ConditionTrue,
			enterpriseApi.ReasonNoahDependencyResolved,
			"Referenced NoahCluster and authentication Secret resolved",
		)

		condition.ObservedGeneration = cr.GetGeneration()
		*conditions = splcommon.UpsertCondition(*conditions, condition)

		return runtime, nil
	}

	outcome, handled := noahDependencyOutcome(err)
	if !handled {
		return nil, err
	}

	condition := newNoahDependencyResolvedCondition(outcome.conditionStatus, outcome.conditionReason, outcome.message)
	condition.ObservedGeneration = cr.GetGeneration()
	*conditions = splcommon.UpsertCondition(*conditions, condition)

	return nil, err
}

func noahDependencyOutcome(err error) (noahDependencyReconcileOutcome, bool) {
	dependencyErr, ok := errors.AsType[*configworkflow.NoahDependencyError](err)
	if !ok {
		return noahDependencyReconcileOutcome{}, false
	}

	if dependencyErr.Kind() == configworkflow.NoahDependencyMissing {
		return noahDependencyReconcileOutcome{
			phase:           enterpriseApi.PhasePending,
			message:         fmt.Sprintf("Waiting for Noah dependencies: %v", dependencyErr),
			conditionReason: enterpriseApi.ReasonNoahDependencyMissing,
			conditionStatus: metav1.ConditionUnknown,
		}, true
	}

	message := fmt.Sprintf("Invalid Noah dependency configuration: %v", dependencyErr)
	return noahDependencyReconcileOutcome{
		phase:           enterpriseApi.PhaseError,
		message:         message,
		conditionReason: enterpriseApi.ReasonNoahConfigurationInvalid,
		conditionStatus: metav1.ConditionFalse,
		err: splcommon.NewTerminalError(
			EventReasonNoahConfigurationInvalid,
			"Noah dependency validation failed",
			dependencyErr,
		),
	}, true
}
