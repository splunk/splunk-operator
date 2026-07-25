// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"
	"reflect"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const searchHeadPodDisruptionBudgetSuffix = "-pdb"

func getSearchHeadPodDisruptionBudget(
	cr *enterpriseApi.SearchHeadCluster,
	statefulSet *appsv1.StatefulSet,
) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.FromInt32(1)
	controller := true
	blockOwnerDeletion := true

	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSet.GetName() + searchHeadPodDisruptionBudgetSuffix,
			Namespace: statefulSet.GetNamespace(),
			Labels:    statefulSet.GetLabels(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         enterpriseApi.GroupVersion.String(),
					Kind:               "SearchHeadCluster",
					Name:               cr.GetName(),
					UID:                cr.GetUID(),
					Controller:         &controller,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       statefulSet.Spec.Selector.DeepCopy(),
		},
	}
}

func applySearchHeadPodDisruptionBudget(
	ctx context.Context,
	controllerClient splcommon.ControllerClient,
	cr *enterpriseApi.SearchHeadCluster,
	statefulSet *appsv1.StatefulSet,
) error {
	desired := getSearchHeadPodDisruptionBudget(cr, statefulSet)
	current := &policyv1.PodDisruptionBudget{}
	err := controllerClient.Get(
		ctx,
		types.NamespacedName{
			Namespace: desired.GetNamespace(),
			Name:      desired.GetName(),
		},
		current,
	)
	if k8serrors.IsNotFound(err) {
		return controllerClient.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("read Search Head PodDisruptionBudget: %w", err)
	}

	if !metav1.IsControlledBy(current, cr) {
		return fmt.Errorf(
			"PodDisruptionBudget %s already exists and is not controlled by SearchHeadCluster %s",
			current.GetName(),
			cr.GetName(),
		)
	}
	if reflect.DeepEqual(current.Spec, desired.Spec) &&
		reflect.DeepEqual(current.Labels, desired.Labels) {
		return nil
	}

	current.Spec = desired.Spec
	current.Labels = desired.Labels
	if err := controllerClient.Update(ctx, current); err != nil {
		return fmt.Errorf("update Search Head PodDisruptionBudget: %w", err)
	}
	return nil
}
