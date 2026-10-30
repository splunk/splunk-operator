// Copyright (c) 2026 Splunk Inc. All rights reserved.
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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const postgresDatabaseInvalidReason = "PostgresDatabaseConfigurationInvalid"

// ApplyPostgresDatabaseBundle resolves a new projection, or retains the previous
// one during a transient producer outage if the requested bindings are unchanged.
// It never writes producer resources or reuses mounts for changed credentials.
func ApplyPostgresDatabaseBundle(ctx context.Context, c client.Reader, cr client.Object, specs []v4.PostgresDatabaseMountSpec, volumes []corev1.Volume, conditions *[]metav1.Condition, desired *appsv1.StatefulSet) (err error) {
	if len(specs) == 0 {
		meta.RemoveStatusCondition(conditions, string(v4.ConditionPostgresDatabaseResolved))
		return nil
	}
	var dependencyErr error
	retained := false
	defer func() {
		cause := err
		if cause == nil {
			cause = dependencyErr
		}
		condition := metav1.Condition{
			Type: string(v4.ConditionPostgresDatabaseResolved), Status: metav1.ConditionTrue,
			Reason: "PostgresDatabaseResolved", Message: "Referenced databases and published resources resolved",
			ObservedGeneration: cr.GetGeneration(),
		}
		if cause != nil {
			condition.Status = metav1.ConditionFalse
			condition.Reason, condition.Message = "PostgresDatabaseResolutionFailed", cause.Error()
			switch {
			case errors.Is(cause, configworkflow.ErrPostgresDatabasePending):
				condition.Reason = "WaitingForPostgresDatabase"
			case errors.Is(cause, configworkflow.ErrPostgresDatabaseInvalid):
				condition.Reason = postgresDatabaseInvalidReason
			}
			if retained {
				condition.Message += "; retaining the previously resolved projection while workload updates proceed"
			}
		}
		previous := meta.FindStatusCondition(*conditions, condition.Type)
		warn := condition.Reason == postgresDatabaseInvalidReason &&
			(previous == nil || previous.Status != condition.Status || previous.Reason != condition.Reason || previous.Message != condition.Message || previous.ObservedGeneration != condition.ObservedGeneration)
		*conditions = splcommon.UpsertCondition(*conditions, condition)
		if publisher := splcommon.GetEventPublisher(ctx); warn && publisher != nil {
			publisher.Warning(ctx, postgresDatabaseInvalidReason, condition.Message)
		}
	}()
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		return fmt.Errorf("%w: postgresDatabaseMounts requires the PostgresController feature gate", configworkflow.ErrPostgresDatabaseInvalid)
	}
	mounts, dependencyErr := configworkflow.ResolvePostgresDatabaseMounts(ctx, c, cr.GetNamespace(), specs, volumes...)
	if dependencyErr != nil && !errors.Is(dependencyErr, configworkflow.ErrPostgresDatabasePending) {
		return dependencyErr
	}
	binding, err := postgresDatabaseBinding(specs)
	if err != nil {
		return err
	}
	if dependencyErr == nil {
		if err := mounts.Inject(&desired.Spec.Template); err != nil {
			return fmt.Errorf("%w: %v", configworkflow.ErrPostgresDatabaseInvalid, err)
		}
	} else {
		if desired == nil || desired.Name == "" {
			return dependencyErr
		}
		previous := &appsv1.StatefulSet{}
		if readErr := c.Get(ctx, client.ObjectKeyFromObject(desired), previous); readErr != nil {
			if apierrors.IsNotFound(readErr) {
				return dependencyErr
			}
			return fmt.Errorf("read previous PostgreSQL projection: %w", readErr)
		}
		if !metav1.IsControlledBy(previous, cr) {
			return dependencyErr
		}
		retained, err = resources.PreservePostgresDatabaseMounts(&previous.Spec.Template, &desired.Spec.Template, binding)
		if err != nil {
			return fmt.Errorf("%w: %v", configworkflow.ErrPostgresDatabaseInvalid, err)
		}
		if !retained {
			return dependencyErr
		}
	}
	if desired.Spec.Template.Annotations == nil {
		desired.Spec.Template.Annotations = make(map[string]string)
	}
	desired.Spec.Template.Annotations[splcommon.PostgresDatabaseBindingAnnotation] = binding
	return nil
}

// PostgresDatabaseWorkloadPhase keeps Ready false while a retained projection
// lets an image, scale, or resource update converge independently of PostgreSQL.
func PostgresDatabaseWorkloadPhase(phase v4.Phase, message string, conditions []metav1.Condition) (v4.Phase, string) {
	if phase == v4.PhaseReady {
		if dependency := meta.FindStatusCondition(conditions, string(v4.ConditionPostgresDatabaseResolved)); dependency != nil && dependency.Status != metav1.ConditionTrue {
			return v4.PhasePending, dependency.Message
		}
	}
	return phase, message
}

func postgresDatabaseBinding(specs []v4.PostgresDatabaseMountSpec) (string, error) {
	type binding struct {
		Reference, Database string
		IncludeAdmin        bool
	}
	bindings := make([]binding, 0, len(specs))
	for _, spec := range specs {
		bindings = append(bindings, binding{Reference: spec.PostgresDatabaseRef.Name, Database: spec.Database, IncludeAdmin: spec.IncludeAdmin})
	}
	slices.SortFunc(bindings, func(a, b binding) int { return cmp.Compare(a.Database, b.Database) })
	data, err := json.Marshal(bindings)
	if err != nil {
		return "", fmt.Errorf("encode PostgreSQL mount bindings: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
