/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package cnpg wraps CloudNativePG resource operations.
package cnpg

import (
	"context"
	"crypto/sha256"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	MonitoringCMKey = "queries.yaml"

	// Avoids writes and exporter reloads for unchanged content.
	MonitoringCMHashAnnotation     = "enterprise.splunk.com/monitoring-config-hash"
	MonitoringQueryCountAnnotation = "enterprise.splunk.com/monitoring-query-count"
	MonitoringEnabledAnnotation    = "enterprise.splunk.com/monitoring-enabled"
)

type MonitoringConfig struct {
	// Empty YAML disables managed custom metrics.
	YAML   string
	Hash   string
	CMName string
}

type MetricEntry struct {
	Name            string                  `json:"name,omitempty"`
	Query           string                  `json:"query"`
	Metrics         []map[string]MetricSpec `json:"metrics"`
	TargetDatabases []string                `json:"target_databases,omitempty"`
}

type MetricSpec struct {
	Usage       string `json:"usage"`
	Description string `json:"description,omitempty"`
}

// SerializeEntries enforces Kubernetes' immutable 1 MiB object-data limit
// before any ConfigMap write.
func SerializeEntries(entries map[string]MetricEntry) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(entries)
	if err != nil {
		return "", err
	}
	payload := len(MonitoringCMKey) + len(out)
	if payload > corev1.MaxSecretSize {
		return "", fmt.Errorf("%w: generated ConfigMap data is %d bytes; maximum is %d bytes",
			monitoring.ErrGeneratedConfigTooLarge, payload, corev1.MaxSecretSize)
	}
	return string(out), nil
}

func BuildMonitoringConfig(clusterName, yamlContent string) MonitoringConfig {
	return MonitoringConfig{
		YAML:   yamlContent,
		Hash:   hashContent(yamlContent),
		CMName: generatedCMName(clusterName),
	}
}

// Empty YAML removes only owned state; foreign same-named ConfigMaps are never adopted.
func ApplyMonitoringConfig(ctx context.Context, c client.Client, scheme *runtime.Scheme, cnpgCluster *cnpgv1.Cluster, mc MonitoringConfig) (bool, error) {
	existing := &corev1.ConfigMap{}
	getErr := c.Get(ctx, types.NamespacedName{Name: mc.CMName, Namespace: cnpgCluster.Namespace}, existing)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return false, fmt.Errorf("getting generated metrics ConfigMap: %w", getErr)
	}

	if mc.YAML == "" {
		if apierrors.IsNotFound(getErr) {
			return patchClusterSelector(ctx, c, cnpgCluster, mc.CMName, false)
		}
		if err := ensureControlledBy(existing, cnpgCluster); err != nil {
			if clusterSelectorCount(cnpgCluster, mc.CMName) != 0 {
				return false, err
			}
			return false, nil
		}
		if _, err := patchClusterSelector(ctx, c, cnpgCluster, mc.CMName, false); err != nil {
			return false, err
		}
		uid := existing.UID
		rv := existing.ResourceVersion
		if err := c.Delete(ctx, existing, client.Preconditions{UID: &uid, ResourceVersion: &rv}); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("deleting generated metrics ConfigMap: %w", err)
		}
		return true, nil
	}

	if getErr == nil {
		if err := ensureControlledBy(existing, cnpgCluster); err != nil {
			if _, disconnectErr := patchClusterSelector(ctx, c, cnpgCluster, mc.CMName, false); disconnectErr != nil {
				return false, fmt.Errorf("disconnecting foreign generated metrics ConfigMap: %w", disconnectErr)
			}
			return false, err
		}
	}

	desiredData := map[string]string{MonitoringCMKey: mc.YAML}
	if getErr == nil &&
		existing.Annotations[MonitoringCMHashAnnotation] == mc.Hash &&
		equality.Semantic.DeepEqual(existing.Data, desiredData) &&
		len(existing.BinaryData) == 0 {
		return patchClusterSelector(ctx, c, cnpgCluster, mc.CMName, true)
	}

	cm := existing
	if apierrors.IsNotFound(getErr) {
		cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: mc.CMName, Namespace: cnpgCluster.Namespace}}
		if err := ctrl.SetControllerReference(cnpgCluster, cm, scheme); err != nil {
			return false, fmt.Errorf("setting generated metrics ConfigMap owner: %w", err)
		}
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	cm.Annotations[MonitoringCMHashAnnotation] = mc.Hash
	cm.Data = desiredData
	cm.BinaryData = nil
	var err error
	if apierrors.IsNotFound(getErr) {
		err = c.Create(ctx, cm)
	} else {
		err = c.Update(ctx, cm)
	}
	if err != nil {
		return false, fmt.Errorf("writing generated metrics ConfigMap: %w", err)
	}
	if _, err := patchClusterSelector(ctx, c, cnpgCluster, mc.CMName, true); err != nil {
		return false, err
	}
	return true, nil
}

func ObserveMonitoringConfig(
	ctx context.Context,
	c client.Client,
	cnpgCluster *cnpgv1.Cluster,
	revision string,
	enabled bool,
) (bool, string, error) {
	cm := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      generatedCMName(cnpgCluster.Name),
		Namespace: cnpgCluster.Namespace,
	}, cm)

	selectorCount := clusterSelectorCount(cnpgCluster, generatedCMName(cnpgCluster.Name))
	if !enabled {
		if err != nil && !apierrors.IsNotFound(err) {
			return false, "", fmt.Errorf("getting generated metrics ConfigMap: %w", err)
		}
		if selectorCount != 0 {
			return false, "waiting for the CNPG custom-metrics selector to be removed", nil
		}
		if err == nil && !isControlledBy(cm, cnpgCluster) {
			return true, "", nil
		}
		if _, found := cnpgCluster.Status.ConfigMapResourceVersion.Metrics[generatedCMName(cnpgCluster.Name)]; found {
			return false, "waiting for CNPG to observe custom-metrics disablement", nil
		}
		if err == nil && isControlledBy(cm, cnpgCluster) {
			return false, "waiting for the generated metrics ConfigMap to be removed", nil
		}
		return true, "", nil
	}

	if apierrors.IsNotFound(err) {
		return false, "waiting for the generated metrics ConfigMap", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("getting generated metrics ConfigMap: %w", err)
	}
	if err := ensureControlledBy(cm, cnpgCluster); err != nil {
		return false, "", err
	}
	yamlContent, ok := exactMonitoringData(cm)
	if !ok {
		return false, "waiting for generated metrics ConfigMap data drift to be repaired", nil
	}
	if cm.Annotations[MonitoringCMHashAnnotation] != revision || hashContent(yamlContent) != revision {
		return false, "waiting for the expected generated metrics revision", nil
	}
	if selectorCount != 1 {
		return false, "waiting for exactly one CNPG custom-metrics selector", nil
	}
	if cnpgCluster.Status.ConfigMapResourceVersion.Metrics[cm.Name] != cm.ResourceVersion {
		return false, "waiting for CNPG to observe the generated metrics ConfigMap revision", nil
	}
	return true, "", nil
}

func patchClusterSelector(ctx context.Context, c client.Client, cnpgCluster *cnpgv1.Cluster, cmName string, enabled bool) (bool, error) {
	var current []machineryapi.ConfigMapKeySelector
	if cnpgCluster.Spec.Monitoring != nil {
		current = cnpgCluster.Spec.Monitoring.CustomQueriesConfigMap
	}
	desired := make([]machineryapi.ConfigMapKeySelector, 0, len(current)+1)
	for _, sel := range current {
		if sel.Name == cmName && sel.Key == MonitoringCMKey {
			continue
		}
		desired = append(desired, sel)
	}
	if enabled {
		desired = append(desired, machineryapi.ConfigMapKeySelector{
			LocalObjectReference: machineryapi.LocalObjectReference{Name: cmName},
			Key:                  MonitoringCMKey,
		})
	}
	if equalSelectors(current, desired) {
		return false, nil
	}
	patch := client.MergeFromWithOptions(
		cnpgCluster.DeepCopy(),
		client.MergeFromWithOptimisticLock{},
	)
	if cnpgCluster.Spec.Monitoring == nil {
		if len(desired) == 0 {
			return false, nil
		}
		cnpgCluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{}
	}
	cnpgCluster.Spec.Monitoring.CustomQueriesConfigMap = desired
	if err := c.Patch(ctx, cnpgCluster, patch); err != nil {
		return false, fmt.Errorf("patching CNPG cluster monitoring selector: %w", err)
	}
	return true, nil
}

func ensureControlledBy(cm *corev1.ConfigMap, owner *cnpgv1.Cluster) error {
	ref := metav1.GetControllerOf(cm)
	if ref == nil || ref.APIVersion != cnpgv1.SchemeGroupVersion.String() ||
		ref.Kind != "Cluster" || ref.Name != owner.Name || ref.UID != owner.UID {
		return fmt.Errorf("%w: generated metrics ConfigMap %s/%s is not controlled by CNPG Cluster %s (uid %s)",
			monitoring.ErrGeneratedResourceOwnershipConflict,
			cm.Namespace, cm.Name, owner.Name, owner.UID)
	}
	return nil
}

func isControlledBy(cm *corev1.ConfigMap, owner *cnpgv1.Cluster) bool {
	return ensureControlledBy(cm, owner) == nil
}

func exactMonitoringData(cm *corev1.ConfigMap) (string, bool) {
	if len(cm.Data) != 1 || len(cm.BinaryData) != 0 {
		return "", false
	}
	value, ok := cm.Data[MonitoringCMKey]
	return value, ok
}

func clusterSelectorCount(cnpgCluster *cnpgv1.Cluster, cmName string) int {
	if cnpgCluster.Spec.Monitoring == nil {
		return 0
	}
	count := 0
	for _, selector := range cnpgCluster.Spec.Monitoring.CustomQueriesConfigMap {
		if selector.Name == cmName && selector.Key == MonitoringCMKey {
			count++
		}
	}
	return count
}

func equalSelectors(a, b []machineryapi.ConfigMapKeySelector) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Key != b[i].Key {
			return false
		}
	}
	return true
}

func generatedCMName(clusterName string) string { return clusterName + "-metrics" }

func hashContent(s string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s)))
}
