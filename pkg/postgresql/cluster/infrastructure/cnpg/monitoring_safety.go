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

package cnpg

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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

type MonitoringSnapshot struct {
	YAML       string
	Hash       string
	QueryCount int
}

func ReadMonitoringSnapshot(
	ctx context.Context,
	c client.Client,
	cnpgCluster *cnpgv1.Cluster,
	expectedRevision string,
	queryCount int,
) (MonitoringSnapshot, error) {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      generatedCMName(cnpgCluster.Name),
		Namespace: cnpgCluster.Namespace,
	}, cm); err != nil {
		return MonitoringSnapshot{}, fmt.Errorf("getting generated metrics ConfigMap for safety snapshot: %w", err)
	}
	if err := ensureControlledBy(cm, cnpgCluster); err != nil {
		return MonitoringSnapshot{}, err
	}
	yamlContent, ok := exactMonitoringData(cm)
	if !ok || cm.Annotations[MonitoringCMHashAnnotation] != expectedRevision ||
		hashContent(yamlContent) != expectedRevision {
		return MonitoringSnapshot{}, fmt.Errorf("generated metrics ConfigMap does not contain confirmed revision %q", expectedRevision)
	}
	return MonitoringSnapshot{YAML: yamlContent, Hash: expectedRevision, QueryCount: queryCount}, nil
}

func SaveMonitoringSnapshot(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner client.Object,
	ownerAPIVersion string,
	ownerKind string,
	providerName string,
	snapshot MonitoringSnapshot,
) (bool, error) {
	name := safetyCMName(providerName)
	key := types.NamespacedName{Name: name, Namespace: owner.GetNamespace()}
	current := &corev1.ConfigMap{}
	err := c.Get(ctx, key, current)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("getting custom-metrics safety ConfigMap: %w", err)
	}
	if err == nil {
		if ownerErr := ensureControlledByObject(current, owner, ownerAPIVersion, ownerKind); ownerErr != nil {
			return false, ownerErr
		}
	}

	managedAnnotations := map[string]string{
		MonitoringCMHashAnnotation:     snapshot.Hash,
		MonitoringEnabledAnnotation:    "true",
		MonitoringQueryCountAnnotation: strconv.Itoa(snapshot.QueryCount),
	}
	data := map[string]string{MonitoringCMKey: snapshot.YAML}
	if err == nil &&
		monitoringAnnotationsEqual(current.Annotations, managedAnnotations) &&
		equality.Semantic.DeepEqual(current.Data, data) &&
		len(current.BinaryData) == 0 {
		return false, nil
	}

	desired := current
	if apierrors.IsNotFound(err) {
		desired = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}}
		if err := ctrl.SetControllerReference(owner, desired, scheme); err != nil {
			return false, fmt.Errorf("setting custom-metrics safety ConfigMap owner: %w", err)
		}
	}
	desired.Annotations = maps.Clone(desired.Annotations)
	if desired.Annotations == nil {
		desired.Annotations = map[string]string{}
	}
	maps.Copy(desired.Annotations, managedAnnotations)
	desired.Data = data
	desired.BinaryData = nil
	if apierrors.IsNotFound(err) {
		err = c.Create(ctx, desired)
	} else {
		err = c.Update(ctx, desired)
	}
	if err != nil {
		return false, fmt.Errorf("writing custom-metrics safety ConfigMap: %w", err)
	}
	return true, nil
}

func DeleteMonitoringSnapshot(
	ctx context.Context,
	c client.Client,
	owner client.Object,
	ownerAPIVersion string,
	ownerKind string,
	providerName string,
) (bool, error) {
	current := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      safetyCMName(providerName),
		Namespace: owner.GetNamespace(),
	}, current)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting custom-metrics safety ConfigMap: %w", err)
	}
	if err := ensureControlledByObject(current, owner, ownerAPIVersion, ownerKind); err != nil {
		return false, err
	}
	uid := current.UID
	rv := current.ResourceVersion
	if err := c.Delete(ctx, current, client.Preconditions{UID: &uid, ResourceVersion: &rv}); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("deleting custom-metrics safety ConfigMap: %w", err)
	}
	return true, nil
}

func LoadMonitoringSnapshot(
	ctx context.Context,
	c client.Client,
	owner client.Object,
	ownerAPIVersion string,
	ownerKind string,
	providerName string,
) (MonitoringSnapshot, bool, string, error) {
	current := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      safetyCMName(providerName),
		Namespace: owner.GetNamespace(),
	}, current)
	if apierrors.IsNotFound(err) {
		return MonitoringSnapshot{}, false, "last-known-good custom metrics are unavailable", nil
	}
	if err != nil {
		return MonitoringSnapshot{}, false, "", fmt.Errorf("getting custom-metrics safety ConfigMap: %w", err)
	}
	if err := ensureControlledByObject(current, owner, ownerAPIVersion, ownerKind); err != nil {
		return MonitoringSnapshot{}, false, "", err
	}
	yamlContent, ok := exactMonitoringData(current)
	revision := current.Annotations[MonitoringCMHashAnnotation]
	queryCount, countErr := strconv.Atoi(current.Annotations[MonitoringQueryCountAnnotation])
	if !ok || current.Annotations[MonitoringEnabledAnnotation] != "true" ||
		revision == "" || hashContent(yamlContent) != revision || countErr != nil || queryCount < 0 {
		return MonitoringSnapshot{}, false, "last-known-good custom metrics safety payload is invalid", nil
	}
	snapshot := MonitoringSnapshot{
		YAML:       yamlContent,
		Hash:       revision,
		QueryCount: queryCount,
	}
	if err := validateMonitoringSnapshot(snapshot); err != nil {
		return MonitoringSnapshot{}, false,
			fmt.Sprintf("last-known-good custom metrics safety payload failed validation: %v", err), nil
	}
	return snapshot, true, "", nil
}

func monitoringAnnotationsEqual(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func validateMonitoringSnapshot(snapshot MonitoringSnapshot) error {
	var entries map[string]MetricEntry
	if err := yaml.UnmarshalStrict([]byte(snapshot.YAML), &entries); err != nil {
		return fmt.Errorf("decoding CNPG query definitions: %w", err)
	}
	if snapshot.QueryCount < 1 {
		return fmt.Errorf("enabled snapshot has invalid query count %d", snapshot.QueryCount)
	}
	if len(entries) != snapshot.QueryCount {
		return fmt.Errorf("query count is %d, expected %d", len(entries), snapshot.QueryCount)
	}
	for key, entry := range entries {
		if !strings.HasPrefix(key, "splunk_operator_") {
			return fmt.Errorf("query key %q is outside the managed namespace", key)
		}
		if !strings.HasPrefix(entry.Name, "splunk_operator_") {
			return fmt.Errorf("query %q provider name %q is outside the managed namespace", key, entry.Name)
		}
		if strings.TrimSpace(entry.Query) == "" {
			return fmt.Errorf("query %q has empty SQL", key)
		}
		if len(entry.Metrics) == 0 {
			return fmt.Errorf("query %q has no metric mappings", key)
		}
		values := 0
		for i, mapping := range entry.Metrics {
			if len(mapping) != 1 {
				return fmt.Errorf("query %q metric mapping %d must contain exactly one column", key, i)
			}
			for column, spec := range mapping {
				if strings.TrimSpace(column) == "" {
					return fmt.Errorf("query %q metric mapping %d has an empty column", key, i)
				}
				switch spec.Usage {
				case "GAUGE", "COUNTER":
					values++
				case "LABEL":
				case "":
					return fmt.Errorf("query %q column %q has empty usage", key, column)
				default:
					return fmt.Errorf("query %q column %q has unsupported usage %q", key, column, spec.Usage)
				}
			}
		}
		if values != 1 {
			return fmt.Errorf("query %q has %d value columns, expected 1", key, values)
		}
		if len(entry.TargetDatabases) > 1 {
			return fmt.Errorf("query %q targets more than one database", key)
		}
		if len(entry.TargetDatabases) == 1 && strings.TrimSpace(entry.TargetDatabases[0]) == "" {
			return fmt.Errorf("query %q has an empty target database", key)
		}
	}
	return nil
}

func ensureControlledByObject(
	cm *corev1.ConfigMap,
	owner client.Object,
	ownerAPIVersion string,
	ownerKind string,
) error {
	ref := metav1.GetControllerOf(cm)
	if ref == nil || ref.APIVersion != ownerAPIVersion || ref.Kind != ownerKind ||
		ref.Name != owner.GetName() || ref.UID != owner.GetUID() {
		return fmt.Errorf("custom-metrics safety ConfigMap %s/%s is not controlled by %s %s (uid %s)",
			cm.Namespace, cm.Name, ownerKind, owner.GetName(), owner.GetUID())
	}
	return nil
}

func safetyCMName(clusterName string) string {
	return clusterName + "-metrics-lkg"
}
