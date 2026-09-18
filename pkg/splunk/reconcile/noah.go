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

package reconcile

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

// NoahDependencyResult contains the resolved runtime or the reconciliation
// state to report when the dependency is unavailable.
type NoahDependencyResult struct {
	Runtime *configworkflow.NoahRuntime
	Phase   enterpriseApi.Phase
	Message string
	// StateKnown distinguishes classified missing or invalid dependencies from
	// API failures where the dependency state could not be determined.
	StateKnown   bool
	ReconcileErr error
}

// ResolveNoahDependency resolves the referenced NoahCluster and authentication
// Secret and records the result in the workload's conditions.
func ResolveNoahDependency(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, conditions *[]metav1.Condition, ref *corev1.LocalObjectReference) NoahDependencyResult {
	if ref == nil || ref.Name == "" {
		result := invalidNoahDependencyResult(fmt.Errorf("noahClusterRef.name must not be empty"))
		setNoahDependencyCondition(cr, conditions, metav1.ConditionFalse, enterpriseApi.ReasonNoahConfigurationInvalid, result.Message)
		return result
	}

	runtime, err := configworkflow.ResolveNoahRuntime(ctx, client, cr.GetNamespace(), *ref)
	if err == nil {
		setNoahDependencyCondition(cr, conditions, metav1.ConditionTrue, enterpriseApi.ReasonNoahDependencyResolved, "Referenced NoahCluster and authentication Secret resolved")
		return NoahDependencyResult{Runtime: runtime, StateKnown: true}
	}

	dependencyErr, classified := errors.AsType[*configworkflow.NoahDependencyError](err)
	if !classified {
		result := NoahDependencyResult{
			Phase:        enterpriseApi.PhaseError,
			Message:      fmt.Sprintf("Unable to determine Noah dependency state: %v", err),
			ReconcileErr: err,
		}
		setNoahDependencyCondition(cr, conditions, metav1.ConditionUnknown, enterpriseApi.ReasonNoahDependencyUnknown, result.Message)
		return result
	}

	if dependencyErr.Kind() == configworkflow.NoahDependencyMissing {
		result := NoahDependencyResult{
			Phase:      enterpriseApi.PhasePending,
			Message:    fmt.Sprintf("Waiting for Noah dependencies: %v", dependencyErr),
			StateKnown: true,
		}
		setNoahDependencyCondition(cr, conditions, metav1.ConditionUnknown, enterpriseApi.ReasonNoahDependencyMissing, result.Message)
		return result
	}

	result := invalidNoahDependencyResult(dependencyErr)
	setNoahDependencyCondition(cr, conditions, metav1.ConditionFalse, enterpriseApi.ReasonNoahConfigurationInvalid, result.Message)
	return result
}

func invalidNoahDependencyResult(err error) NoahDependencyResult {
	return NoahDependencyResult{
		Phase:      enterpriseApi.PhaseError,
		Message:    fmt.Sprintf("Invalid Noah dependency configuration: %v", err),
		StateKnown: true,
		ReconcileErr: splcommon.NewTerminalError(
			splcommon.EventReasonNoahConfigurationInvalid,
			"Noah dependency validation failed",
			err,
		),
	}
}

func setNoahDependencyCondition(cr splcommon.MetaObject, conditions *[]metav1.Condition, status metav1.ConditionStatus, reason enterpriseApi.ConditionReason, message string) {
	condition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionNoahDependencyResolved),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: cr.GetGeneration(),
	}
	*conditions = splcommon.UpsertCondition(*conditions, condition)
}
