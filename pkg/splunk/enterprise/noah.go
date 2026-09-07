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
	"errors"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/noah"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func noahDependencyOutcome(err error) (noahDependencyReconcileOutcome, bool) {
	var dependencyErr *noah.DependencyError
	if !errors.As(err, &dependencyErr) {
		return noahDependencyReconcileOutcome{}, false
	}

	if dependencyErr.Kind() == noah.DependencyMissing {
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
			EventReasonValidateSpecFailed,
			"Noah dependency validation failed",
			dependencyErr,
		),
	}, true
}
