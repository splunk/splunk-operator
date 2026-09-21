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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const indexerClusterMaxUnavailable = 1

func applyPodDisruptionBudget(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) error {
	err := k8sops.ApplyPodDisruptionBudget(ctx, client, newPodDisruptionBudget(cr))
	if err == nil {
		return nil
	}

	k8sops.GetEventPublisher(ctx, cr).Warning(ctx, "ApplyPodDisruptionBudgetFailed", "Create or update of PodDisruptionBudget failed. Check operator logs for details.")

	status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
		Phase: enterpriseApi.PhaseError, Message: "Failed to create or update PodDisruptionBudget", Generation: cr.GetGeneration(),
	})
	cr.Status.Phase = status.Phase
	cr.Status.Conditions = status.Conditions
	cr.Status.ObservedGeneration = cr.GetGeneration()

	if statusErr := client.Status().Update(ctx, cr); statusErr != nil {
		return errors.Join(fmt.Errorf("apply PodDisruptionBudget: %w", err), fmt.Errorf("update IndexerCluster status: %w", statusErr))
	}

	return fmt.Errorf("apply PodDisruptionBudget: %w", err)
}

func newPodDisruptionBudget(cr *enterpriseApi.IndexerCluster) *policyv1.PodDisruptionBudget {
	instance := splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, cr.Name)

	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance":   instance,
				"app.kubernetes.io/managed-by": "splunk-operator",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("IndexerCluster")),
			},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(indexerClusterMaxUnavailable)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/instance": instance},
			},
		},
	}
}
