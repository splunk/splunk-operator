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

package k8sops

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// GetKVServiceDeployment resolves API-backed configuration before returning a complete workload.
func GetKVServiceDeployment(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) (*appsv1.Deployment, error) {
	desired := resources.NewKVServiceDeployment(cr)
	current := &appsv1.Deployment{}
	err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: desired.Name}, current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}
	if err == nil {
		if owner := metav1.GetControllerOf(current); owner != nil && owner.UID != cr.UID {
			return nil, fmt.Errorf("deployment %s is controlled by another resource", current.Name)
		}
		desired.ObjectMeta = *current.ObjectMeta.DeepCopy()
		found := false
		for _, ref := range desired.OwnerReferences {
			if ref.UID == cr.UID {
				found = true
			}
		}
		if !found {
			desired.OwnerReferences = append(desired.OwnerReferences, splcommon.AsOwner(cr, true))
		}
	}
	if cr.Spec.ServiceAccount != "" {
		if _, err := GetServiceAccount(ctx, c, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Spec.ServiceAccount}); err != nil {
			return nil, err
		}
	}
	secret, err := splutil.GetLatestVersionedSecret(ctx, c, cr, cr.Namespace, desired.Name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, fmt.Errorf("deployment secret for %s is unavailable", desired.Name)
	}
	spec := cr.Spec.CommonSplunkSpec.DeepCopy()
	if err := updateSplunkPodTemplateWithConfig(ctx, c, &desired.Spec.Template, cr, spec, splcommon.SplunkKVService, nil, secret.Name); err != nil {
		return nil, err
	}
	resources.ConfigureKVServiceHTTPProbes(&desired.Spec.Template)
	return desired, nil
}

// UpdateKVServiceStatus retries conflicts without overwriting concurrent spec updates.
func UpdateKVServiceStatus(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &enterpriseApi.KVService{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, latest); err != nil {
			return err
		}
		cr.Status.DeepCopyInto(&latest.Status)
		return c.Status().Update(ctx, latest)
	})
}

// DeleteKVServiceCR removes the current CR as an owner of the shared KVService CR.
// If the shared KVService has no owners left, it is deleted.
func DeleteKVServiceCR(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject) error {
	logger := logging.FromContext(ctx).With("func", "DeleteKVServiceCR", "kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	kvService := &enterpriseApi.KVService{}
	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      splcommon.KVServiceCrName,
	}
	if err := c.Get(ctx, namespacedName, kvService); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		logger.ErrorContext(ctx, "failed to get KVService CR", "error", err)
		return err
	}

	if kvService.GetUID() == cr.GetUID() {
		return nil
	}

	ownerRefs := kvService.GetOwnerReferences()
	remainingRefs := removeOwnerReference(ownerRefs, cr)
	if len(remainingRefs) == len(ownerRefs) {
		return nil
	}

	if len(remainingRefs) == 0 {
		if err := c.Delete(ctx, kvService); err != nil && !k8serrors.IsNotFound(err) {
			logger.ErrorContext(ctx, "failed to delete KVService CR", "error", err)
			return err
		}
		return nil
	}

	kvService.SetOwnerReferences(remainingRefs)
	if err := c.Update(ctx, kvService); err != nil {
		logger.ErrorContext(ctx, "failed to update KVService owner references", "error", err)
		return err
	}
	return nil
}

func removeOwnerReference(ownerRefs []metav1.OwnerReference, cr splcommon.MetaObject) []metav1.OwnerReference {
	remainingRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))
	for _, ref := range ownerRefs {
		if ref.UID != cr.GetUID() {
			remainingRefs = append(remainingRefs, ref)
		}
	}
	return remainingRefs
}
