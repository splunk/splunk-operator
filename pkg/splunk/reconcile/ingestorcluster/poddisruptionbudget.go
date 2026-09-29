// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package ingestorcluster

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func applyPodDisruptionBudget(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) error {
	instanceLabel := fmt.Sprintf("splunk-%s-ingestor", cr.GetName())
	maxUnavailable := intstr.FromInt(1)
	desired := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkIngestor, cr.GetName()),
			Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"app.kubernetes.io/instance":   instanceLabel,
			},
			OwnerReferences: []metav1.OwnerReference{splcommon.AsOwner(cr, true)},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": instanceLabel,
				},
			},
		},
	}

	var list policyv1.PodDisruptionBudgetList
	if err := c.List(ctx, &list,
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels{
			"app.kubernetes.io/managed-by": "splunk-operator",
			"app.kubernetes.io/instance":   instanceLabel,
		},
	); err != nil {
		return err
	}
	if len(list.Items) == 0 {
		return splutil.CreateResource(ctx, c, desired)
	}
	for _, pdb := range list.Items {
		for _, ref := range pdb.OwnerReferences {
			if ref.UID == cr.GetUID() {
				return nil
			}
		}
	}
	return fmt.Errorf("PodDisruptionBudget for IngestorCluster %q exists in namespace %q but is not owned by this CR",
		cr.GetName(), cr.GetNamespace())
}
