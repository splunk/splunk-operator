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

// EnsureConfigMap ensures that an immutable defaults.yml ConfigMap for the
// given CR and config entries exists in the cluster.
//
// It returns a DefaultsConfigMap whose name is content-addressed, so a
// different config produces a different name and a new ConfigMap is created.
//
// owner, when non-nil, is set as an owner reference on the ConfigMap at
// creation time. Pass splcommon.AsOwner(cr, true) from the reconciler.
//
// If a ConfigMap with the desired name already exists its content is verified
// against the desired entries. A mismatch indicates a hash collision and is
// returned as an error — the caller should treat this as a hard failure.
func EnsureConfigMap(ctx context.Context, c client.Client, cr client.Object, entries []common.ConfFileEntry, owner *metav1.OwnerReference) (resources.DefaultsConfigMap, error) {
	desired, err := resources.NewDefaultsConfigMap(cr, entries, owner)
	if err != nil {
		return resources.DefaultsConfigMap{}, err
	}
	ns := desired.Namespace

	var existing corev1.ConfigMap
	err = c.Get(ctx, client.ObjectKey{Namespace: ns, Name: desired.Name}, &existing)
	if err == nil {
		if err := verifyContent(&existing, &desired.ConfigMap); err != nil {
			return resources.DefaultsConfigMap{}, reconcile.TerminalError(fmt.Errorf("defaults ConfigMap %s/%s exists with different content (possible hash collision): %w", ns, desired.Name, err))
		}
		return desired, nil
	}
	if !k8serrors.IsNotFound(err) {
		return resources.DefaultsConfigMap{}, fmt.Errorf("get defaults ConfigMap %s/%s: %w", ns, desired.Name, err)
	}

	if err := c.Create(ctx, &desired.ConfigMap); err != nil {
		return resources.DefaultsConfigMap{}, fmt.Errorf("create defaults ConfigMap %s/%s: %w", ns, desired.Name, err)
	}
	return desired, nil
}

// GarbageCollectConfigMaps deletes all SOK defaults ConfigMaps in namespace
// that belong to the given CR but are not currentName.
//
// podSelector should be the managing StatefulSet's Spec.Selector — it is used to
// list the CR's pods so a stale ConfigMap that is still mounted by an in-flight
// pod is skipped rather than deleted. A nil selector (or one with empty
// MatchLabels) lists every pod in the namespace, which is the conservative
// direction: it can only keep more ConfigMaps alive, never delete a mounted one.
// The defaults ConfigMaps themselves are identified by the {crKind, crName}
// labels they carry.
//
// This must be called only after the StatefulSet referencing currentName has
// been successfully applied, so that old ConfigMaps are no longer mounted on
// any in-flight pods. Failures are logged but do not block the reconcile —
// orphaned ConfigMaps are harmless and will be collected on the next successful
// reconcile.
func GarbageCollectConfigMaps(ctx context.Context, c client.Client, cr client.Object, currentName string, podSelector *metav1.LabelSelector) {
	logger := log.FromContext(ctx)
	namespace := cr.GetNamespace()
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	crName := cr.GetName()

	var cmList corev1.ConfigMapList
	if err := c.List(ctx, &cmList,
		client.InNamespace(namespace),
		client.MatchingLabels{resources.LabelCRName: crName, resources.LabelCRKind: crKind},
	); err != nil {
		logger.Error(err, "GarbageCollectConfigMaps: list failed", "namespace", namespace, "crKind", crKind, "crName", crName)
		return
	}

	stale := make([]*corev1.ConfigMap, 0, len(cmList.Items))
	for i := range cmList.Items {
		if cmList.Items[i].Name != currentName {
			stale = append(stale, &cmList.Items[i])
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
		logger.Error(err, "GarbageCollectConfigMaps: list pods failed", "namespace", namespace)
		return
	}

	for _, cm := range stale {
		if podReferencesConfigMap(podList.Items, cm.Name) {
			logger.Info("GarbageCollectConfigMaps: skipping stale ConfigMap still mounted by a pod", "name", cm.Name)
			continue
		}
		if err := c.Delete(ctx, cm); err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "GarbageCollectConfigMaps: delete failed", "name", cm.Name)
		} else {
			logger.Info("GarbageCollectConfigMaps: deleted stale ConfigMap", "name", cm.Name)
		}
	}
}

// selectorLabels returns the MatchLabels of a StatefulSet pod selector, tolerating
// a nil selector. It never returns a nil map's methods unsafely: a nil selector
// yields a nil map, which client.MatchingLabels treats as "match everything".
func selectorLabels(selector *metav1.LabelSelector) map[string]string {
	if selector == nil {
		return nil
	}
	return selector.MatchLabels
}

// podReferencesConfigMap returns true if any pod in the list mounts a volume backed by cmName.
func podReferencesConfigMap(pods []corev1.Pod, cmName string) bool {
	for i := range pods {
		for _, vol := range pods[i].Spec.Volumes {
			if vol.ConfigMap != nil && vol.ConfigMap.Name == cmName {
				return true
			}
		}
	}
	return false
}

// verifyContent returns an error if existing's defaults.yml data differs from desired's.
// A mismatch with the same name means a hash collision — this should never happen in
// practice with SHA-256, but the check keeps correctness independent of hash length.
func verifyContent(existing, desired *corev1.ConfigMap) error {
	const key = "conf-defaults.yml"
	if existing.Data[key] != desired.Data[key] {
		return fmt.Errorf("content mismatch for key %q: possible hash collision", key)
	}
	return nil
}
