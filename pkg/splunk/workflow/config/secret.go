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

package config

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// EnsureSecret ensures that an immutable defaults.yml Secret for the given CR
// and config entries exists in the cluster.
//
// It returns a DefaultsSecret whose name is content-addressed, so a different
// config produces a different name and a new Secret is created.
//
// owner, when non-nil, is set as an owner reference on the Secret at creation
// time. Pass splcommon.AsOwner(cr, true) from the reconciler.
//
// If a Secret with the desired name already exists its content is verified
// against the desired entries. A mismatch indicates a hash collision and is
// returned as an error — the caller should treat this as a hard failure.
func EnsureSecret(ctx context.Context, c client.Client, cr client.Object, entries []common.ConfFileEntry, owner *metav1.OwnerReference) (resources.DefaultsSecret, error) {
	desired, err := resources.NewDefaultsSecret(cr, entries, owner)
	if err != nil {
		return resources.DefaultsSecret{}, err
	}
	ns := desired.Namespace

	var existing corev1.Secret
	err = c.Get(ctx, client.ObjectKey{Namespace: ns, Name: desired.Name}, &existing)
	if err == nil {
		if err := verifySecretContent(&existing, &desired.Secret); err != nil {
			return resources.DefaultsSecret{}, reconcile.TerminalError(fmt.Errorf("defaults Secret %s/%s exists with different content (possible hash collision): %w", ns, desired.Name, err))
		}
		return desired, nil
	}
	if !k8serrors.IsNotFound(err) {
		return resources.DefaultsSecret{}, fmt.Errorf("get defaults Secret %s/%s: %w", ns, desired.Name, err)
	}

	if err := c.Create(ctx, &desired.Secret); err != nil {
		return resources.DefaultsSecret{}, fmt.Errorf("create defaults Secret %s/%s: %w", ns, desired.Name, err)
	}
	return desired, nil
}

// GarbageCollectSecrets deletes all SOK defaults Secrets in namespace that
// belong to the given CR but are not currentName.
//
// podSelector should be the managing StatefulSet's Spec.Selector — it is used to
// list the CR's pods so a stale Secret that is still mounted by an in-flight pod
// is skipped rather than deleted. A nil selector (or one with empty MatchLabels)
// lists every pod in the namespace, which is the conservative direction: it can
// only keep more Secrets alive, never delete a mounted one. The defaults Secrets
// themselves are identified by the {crKind, crName} labels they carry.
//
// This must be called only after the StatefulSet referencing currentName has
// been successfully applied, so that old Secrets are no longer mounted on any
// in-flight pods. Failures are logged but do not block the reconcile — orphaned
// Secrets are harmless and will be collected on the next successful reconcile.
func GarbageCollectSecrets(ctx context.Context, c client.Client, cr client.Object, currentName string, podSelector *metav1.LabelSelector) {
	logger := log.FromContext(ctx)
	namespace := cr.GetNamespace()
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	crName := cr.GetName()

	var secretList corev1.SecretList
	if err := c.List(ctx, &secretList,
		client.InNamespace(namespace),
		client.MatchingLabels{resources.LabelCRName: crName, resources.LabelCRKind: crKind},
	); err != nil {
		logger.Error(err, "GarbageCollectSecrets: list failed", "namespace", namespace, "crKind", crKind, "crName", crName)
		return
	}

	stale := make([]*corev1.Secret, 0, len(secretList.Items))
	for i := range secretList.Items {
		if secretList.Items[i].Name != currentName {
			stale = append(stale, &secretList.Items[i])
		}
	}
	if len(stale) == 0 {
		return
	}

	var podList corev1.PodList
	if err := c.List(ctx, &podList,
		client.InNamespace(namespace),
		client.MatchingLabels(selectorLabels(podSelector)),
	); err != nil {
		logger.Error(err, "GarbageCollectSecrets: list pods failed", "namespace", namespace)
		return
	}

	for _, secret := range stale {
		if podReferencesSecret(podList.Items, secret.Name) {
			logger.Info("GarbageCollectSecrets: skipping stale Secret still mounted by a pod", "name", secret.Name)
			continue
		}
		if err := c.Delete(ctx, secret); err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "GarbageCollectSecrets: delete failed", "name", secret.Name)
		} else {
			logger.Info("GarbageCollectSecrets: deleted stale Secret", "name", secret.Name)
		}
	}
}

// podReferencesSecret returns true if any pod in the list mounts a volume backed by secretName.
func podReferencesSecret(pods []corev1.Pod, secretName string) bool {
	for i := range pods {
		for _, vol := range pods[i].Spec.Volumes {
			if vol.Secret != nil && vol.Secret.SecretName == secretName {
				return true
			}
		}
	}
	return false
}

// verifySecretContent returns an error if existing's defaults.yml data differs from desired's.
// A mismatch with the same name means a hash collision — this should never happen in
// practice with SHA-256, but the check keeps correctness independent of hash length.
func verifySecretContent(existing, desired *corev1.Secret) error {
	const key = "conf-defaults.yml"
	if !bytes.Equal(existing.Data[key], desired.Data[key]) {
		return fmt.Errorf("content mismatch for key %q: possible hash collision", key)
	}
	return nil
}
