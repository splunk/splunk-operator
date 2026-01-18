package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func handleResourceApply(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	obj, err := loadUnstructuredFromStep(step.With)
	if err != nil {
		return nil, err
	}
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	expandUnstructuredObject(obj, exec.Vars)
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		return nil, fmt.Errorf("manifest apiVersion and kind are required")
	}
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if obj.GetNamespace() == "" && namespace != "" {
		obj.SetNamespace(namespace)
	}
	if obj.GetNamespace() == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if obj.GetName() == "" {
		return nil, fmt.Errorf("manifest metadata.name is required")
	}

	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(obj.GetAPIVersion())
	existing.SetKind(obj.GetKind())
	err = exec.Kube.Client.Get(ctx, client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := exec.Kube.Client.Create(ctx, obj); err != nil {
				return nil, err
			}
			return map[string]string{"kind": obj.GetKind(), "name": obj.GetName(), "namespace": obj.GetNamespace(), "action": "created"}, nil
		}
		return nil, err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	if err := exec.Kube.Client.Update(ctx, obj); err != nil {
		return nil, err
	}
	return map[string]string{"kind": obj.GetKind(), "name": obj.GetName(), "namespace": obj.GetNamespace(), "action": "updated"}, nil
}

func handleResourceDelete(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	obj, err := getUnstructuredResource(ctx, exec, step)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]string{"deleted": "false", "reason": "not_found"}, nil
		}
		return nil, err
	}
	if err := exec.Kube.Client.Delete(ctx, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]string{"deleted": "false", "reason": "not_found"}, nil
		}
		return nil, err
	}
	return map[string]string{"kind": obj.GetKind(), "name": obj.GetName(), "namespace": obj.GetNamespace(), "deleted": "true"}, nil
}

func handleResourcePatch(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	obj, err := getUnstructuredResource(ctx, exec, step)
	if err != nil {
		return nil, err
	}
	specPatch, ok := toStringMap(step.With["spec"])
	if ok {
		specPatch = expandMapVars(specPatch, exec.Vars)
		mergeUnstructuredField(obj.Object, "spec", specPatch)
	}
	metaPatch, ok := toStringMap(step.With["metadata"])
	if ok {
		metaPatch = expandMapVars(metaPatch, exec.Vars)
		mergeUnstructuredField(obj.Object, "metadata", metaPatch)
	}
	if err := exec.Kube.Client.Update(ctx, obj); err != nil {
		return nil, err
	}
	return map[string]string{"kind": obj.GetKind(), "name": obj.GetName(), "namespace": obj.GetNamespace()}, nil
}

func handleConfigMapUpdate(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("configmap name is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	rawData, ok := toStringMap(step.With["data"])
	if !ok || len(rawData) == 0 {
		return nil, fmt.Errorf("configmap data is required")
	}
	config := &corev1.ConfigMap{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, config); err != nil {
		return nil, err
	}
	if config.Data == nil {
		config.Data = make(map[string]string, len(rawData))
	}
	for key, value := range rawData {
		config.Data[key] = fmt.Sprintf("%v", value)
	}
	if err := exec.Kube.Client.Update(ctx, config); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "namespace": namespace}, nil
}

func handleAssertConfigMapExists(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("configmap name is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	config := &corev1.ConfigMap{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, config); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "namespace": namespace}, nil
}

func handleAssertConfigMapContains(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("configmap name is required")
	}
	key := expandVars(strings.TrimSpace(getString(step.With, "key", "")), exec.Vars)
	if key == "" {
		return nil, fmt.Errorf("configmap key is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	expected := getBool(step.With, "match", true)
	contains, err := getStringList(step.With, "contains")
	if err != nil {
		return nil, err
	}
	if len(contains) == 0 {
		value := strings.TrimSpace(getString(step.With, "value", ""))
		if value == "" {
			return nil, fmt.Errorf("contains or value is required")
		}
		contains = []string{value}
	}
	contains = expandStringSlice(contains, exec.Vars)

	config := &corev1.ConfigMap{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, config); err != nil {
		return nil, err
	}
	data := config.Data[key]
	for _, item := range contains {
		found := strings.Contains(data, item)
		if found != expected {
			return nil, fmt.Errorf("configmap %s key %s contains %q expected=%t actual=%t", name, key, item, expected, found)
		}
	}
	return map[string]string{"name": name, "key": key}, nil
}

func handleAssertPodCPULimit(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	podName := expandVars(strings.TrimSpace(getString(step.With, "pod", "")), exec.Vars)
	if podName == "" {
		return nil, fmt.Errorf("pod name is required")
	}
	expectedRaw := strings.TrimSpace(getString(step.With, "cpu", ""))
	if expectedRaw == "" {
		return nil, fmt.Errorf("cpu limit is required")
	}
	containerName := expandVars(strings.TrimSpace(getString(step.With, "container", "")), exec.Vars)

	pod := &corev1.Pod{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, err
	}
	expectedQty, err := resource.ParseQuantity(expectedRaw)
	if err != nil {
		return nil, err
	}
	for _, container := range pod.Spec.Containers {
		if containerName != "" && container.Name != containerName {
			continue
		}
		limit := container.Resources.Limits[corev1.ResourceCPU]
		if limit.IsZero() {
			return nil, fmt.Errorf("cpu limit not set on pod %s container %s", podName, container.Name)
		}
		if limit.Cmp(expectedQty) != 0 {
			return nil, fmt.Errorf("cpu limit mismatch on pod %s container %s expected=%s actual=%s", podName, container.Name, expectedQty.String(), limit.String())
		}
		return map[string]string{"pod": podName, "cpu": limit.String()}, nil
	}
	return nil, fmt.Errorf("container not found in pod %s", podName)
}

func handleAssertPodResources(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	podName := expandVars(strings.TrimSpace(getString(step.With, "pod", "")), exec.Vars)
	if podName == "" {
		return nil, fmt.Errorf("pod name is required")
	}
	containerName := expandVars(strings.TrimSpace(getString(step.With, "container", "")), exec.Vars)
	limits, ok := toStringMap(step.With["limits"])
	if !ok {
		limits = map[string]interface{}{}
	}
	requests, ok := toStringMap(step.With["requests"])
	if !ok {
		requests = map[string]interface{}{}
	}
	if len(limits) == 0 && len(requests) == 0 {
		return nil, fmt.Errorf("limits or requests are required")
	}

	pod := &corev1.Pod{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, err
	}

	for _, container := range pod.Spec.Containers {
		if containerName != "" && container.Name != containerName {
			continue
		}
		if err := compareResourceList(container.Resources.Limits, limits, "limits", podName, container.Name); err != nil {
			return nil, err
		}
		if err := compareResourceList(container.Resources.Requests, requests, "requests", podName, container.Name); err != nil {
			return nil, err
		}
		return map[string]string{"pod": podName, "container": container.Name}, nil
	}
	return nil, fmt.Errorf("container not found in pod %s", podName)
}

func handleAssertPodFilesPresent(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	return handleAssertPodFiles(ctx, exec, step, true)
}

func handleAssertPodFilesAbsent(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	return handleAssertPodFiles(ctx, exec, step, false)
}

func handleDeletePod(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	podName := expandVars(strings.TrimSpace(getString(step.With, "pod", "")), exec.Vars)
	labelSelector := expandVars(strings.TrimSpace(getString(step.With, "label_selector", "")), exec.Vars)
	deleted := []string{}
	if podName != "" {
		pod := &corev1.Pod{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
			return nil, err
		}
		if err := exec.Kube.Client.Delete(ctx, pod); err != nil {
			return nil, err
		}
		deleted = append(deleted, podName)
	} else if labelSelector != "" {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			return nil, err
		}
		pods := &corev1.PodList{}
		if err := exec.Kube.Client.List(ctx, pods, &client.ListOptions{Namespace: namespace, LabelSelector: selector}); err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			podCopy := pod
			if err := exec.Kube.Client.Delete(ctx, &podCopy); err != nil {
				return nil, err
			}
			deleted = append(deleted, pod.Name)
		}
	} else {
		return nil, fmt.Errorf("pod or label_selector is required")
	}
	return map[string]string{"namespace": namespace, "deleted": strings.Join(deleted, ",")}, nil
}

func handleAssertPodFiles(ctx context.Context, exec *Context, step spec.StepSpec, expected bool) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	pods, err := getStringList(step.With, "pods")
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
			pods = []string{pod}
		}
	}
	pods = expandStringSlice(pods, exec.Vars)
	if len(pods) == 0 {
		return nil, fmt.Errorf("pod or pods are required")
	}
	files, err := getStringList(step.With, "files")
	if err != nil {
		return nil, err
	}
	paths, err := getStringList(step.With, "paths")
	if err != nil {
		return nil, err
	}
	files = expandStringSlice(files, exec.Vars)
	paths = expandStringSlice(paths, exec.Vars)
	basePath := expandVars(strings.TrimSpace(getString(step.With, "path", "")), exec.Vars)
	if len(files) == 0 && len(paths) == 0 {
		return nil, fmt.Errorf("files or paths are required")
	}

	for _, podName := range pods {
		for _, fileName := range files {
			absPath := fileName
			if basePath != "" {
				absPath = filepath.Join(basePath, fileName)
			}
			if err := assertPodPath(ctx, exec, namespace, podName, absPath, expected); err != nil {
				return nil, err
			}
		}
		for _, path := range paths {
			if err := assertPodPath(ctx, exec, namespace, podName, path, expected); err != nil {
				return nil, err
			}
		}
	}
	return map[string]string{"pods": strings.Join(pods, ","), "expected": fmt.Sprintf("%t", expected)}, nil
}

func handleAssertPodFileContains(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	pods, err := getStringList(step.With, "pods")
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
			pods = []string{pod}
		}
	}
	pods = expandStringSlice(pods, exec.Vars)
	if len(pods) == 0 {
		return nil, fmt.Errorf("pod or pods are required")
	}
	path := expandVars(strings.TrimSpace(getString(step.With, "path", "")), exec.Vars)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	expected := getBool(step.With, "match", true)
	caseInsensitive := getBool(step.With, "case_insensitive", false)
	contains, err := getStringList(step.With, "contains")
	if err != nil {
		return nil, err
	}
	if len(contains) == 0 {
		if value := strings.TrimSpace(getString(step.With, "value", "")); value != "" {
			contains = []string{value}
		}
	}
	if len(contains) == 0 {
		derived, err := getStringList(step.With, "contains_from_pods")
		if err != nil {
			return nil, err
		}
		if len(derived) > 0 {
			usePodIP := getBool(step.With, "use_pod_ip", false)
			derived = expandStringSlice(derived, exec.Vars)
			contains, err = resolvePodIdentifiers(ctx, exec, namespace, derived, usePodIP)
			if err != nil {
				return nil, err
			}
		}
	}
	contains = expandStringSlice(contains, exec.Vars)
	if len(contains) == 0 {
		return nil, fmt.Errorf("contains, value, or contains_from_pods are required")
	}

	for _, podName := range pods {
		stdout, stderr, err := exec.Kube.Exec(ctx, namespace, podName, "", []string{"cat", path}, "", false)
		if err != nil {
			return nil, fmt.Errorf("read pod file failed pod=%s path=%s stderr=%s: %w", podName, path, strings.TrimSpace(stderr), err)
		}
		content := stdout
		if caseInsensitive {
			content = strings.ToLower(content)
		}
		for _, value := range contains {
			needle := value
			if caseInsensitive {
				needle = strings.ToLower(value)
			}
			found := strings.Contains(content, needle)
			if found != expected {
				return nil, fmt.Errorf("pod %s path %s contains %q expected=%t", podName, path, value, expected)
			}
		}
	}
	return map[string]string{"pods": strings.Join(pods, ","), "path": path, "expected": fmt.Sprintf("%t", expected)}, nil
}

func handleAssertPodEnvContains(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	pods, err := getStringList(step.With, "pods")
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
			pods = []string{pod}
		}
	}
	pods = expandStringSlice(pods, exec.Vars)
	if len(pods) == 0 {
		return nil, fmt.Errorf("pod or pods are required")
	}
	contains, err := getStringList(step.With, "contains")
	if err != nil {
		return nil, err
	}
	if len(contains) == 0 {
		if value := strings.TrimSpace(getString(step.With, "value", "")); value != "" {
			contains = []string{value}
		}
	}
	contains = expandStringSlice(contains, exec.Vars)
	if len(contains) == 0 {
		return nil, fmt.Errorf("contains or value is required")
	}
	expected := getBool(step.With, "match", true)
	caseInsensitive := getBool(step.With, "case_insensitive", false)

	for _, podName := range pods {
		stdout, stderr, err := exec.Kube.Exec(ctx, namespace, podName, "", []string{"env"}, "", false)
		if err != nil {
			return nil, fmt.Errorf("env check failed pod=%s stderr=%s: %w", podName, strings.TrimSpace(stderr), err)
		}
		content := stdout
		if caseInsensitive {
			content = strings.ToLower(content)
		}
		for _, value := range contains {
			needle := value
			if caseInsensitive {
				needle = strings.ToLower(value)
			}
			found := strings.Contains(content, needle)
			if found != expected {
				return nil, fmt.Errorf("pod %s env contains %q expected=%t", podName, value, expected)
			}
		}
	}
	return map[string]string{"pods": strings.Join(pods, ","), "expected": fmt.Sprintf("%t", expected)}, nil
}

func resolvePodIdentifiers(ctx context.Context, exec *Context, namespace string, pods []string, usePodIP bool) ([]string, error) {
	if len(pods) == 0 {
		return nil, nil
	}
	if !usePodIP {
		return pods, nil
	}
	identifiers := make([]string, 0, len(pods))
	for _, podName := range pods {
		pod := &corev1.Pod{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
			return nil, err
		}
		if pod.Status.PodIP == "" {
			return nil, fmt.Errorf("pod %s has no IP", podName)
		}
		identifiers = append(identifiers, pod.Status.PodIP)
	}
	return identifiers, nil
}

func handleAssertPVCExists(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	deploymentType := expandVars(strings.TrimSpace(getString(step.With, "deployment_type", "")), exec.Vars)
	if deploymentType == "" {
		return nil, fmt.Errorf("deployment_type is required")
	}
	instances := getInt(step.With, "instances", 1)
	if instances < 1 {
		return nil, fmt.Errorf("instances must be >= 1")
	}
	expected := getBool(step.With, "exists", true)
	baseName := expandVars(strings.TrimSpace(getString(step.With, "base_name", exec.Vars["base_name"])), exec.Vars)
	if baseName == "" {
		return nil, fmt.Errorf("base_name is required")
	}
	kinds, err := getStringList(step.With, "volume_kinds")
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		kinds = []string{"etc", "var"}
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		allMatch := true
		for i := 0; i < instances; i++ {
			for _, kind := range kinds {
				pvcName := fmt.Sprintf("pvc-%s-splunk-%s-%s-%d", kind, baseName, deploymentType, i)
				pvc := &corev1.PersistentVolumeClaim{}
				err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: namespace}, pvc)
				found := err == nil
				if found != expected {
					allMatch = false
				}
			}
		}
		if allMatch {
			return map[string]string{"base_name": baseName, "deployment_type": deploymentType}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("pvc existence did not reach expected state within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func compareResourceList(actual corev1.ResourceList, expected map[string]interface{}, label, podName, containerName string) error {
	for key, value := range expected {
		expectedQty, err := resource.ParseQuantity(fmt.Sprintf("%v", value))
		if err != nil {
			return err
		}
		resourceName := corev1.ResourceName(key)
		actualQty, ok := actual[resourceName]
		if !ok {
			return fmt.Errorf("%s %s not set on pod %s container %s", label, key, podName, containerName)
		}
		if actualQty.Cmp(expectedQty) != 0 {
			return fmt.Errorf("%s %s mismatch on pod %s container %s expected=%s actual=%s", label, key, podName, containerName, expectedQty.String(), actualQty.String())
		}
	}
	return nil
}

func assertPodPath(ctx context.Context, exec *Context, namespace, podName, path string, expected bool) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	stdout, stderr, err := exec.Kube.Exec(ctx, namespace, podName, "", []string{"ls", path}, "", false)
	found := err == nil
	if found != expected {
		return fmt.Errorf("path check failed pod=%s path=%s expected=%t stdout=%s stderr=%s", podName, path, expected, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	}
	return nil
}

func loadUnstructuredFromStep(params map[string]interface{}) (*unstructured.Unstructured, error) {
	if params == nil {
		return nil, fmt.Errorf("manifest parameters are required")
	}
	if raw, ok := params["manifest"]; ok && raw != nil {
		if mapped, ok := raw.(map[string]interface{}); ok {
			return &unstructured.Unstructured{Object: mapped}, nil
		}
		if text, ok := raw.(string); ok {
			return loadUnstructuredFromBytes([]byte(text))
		}
		return nil, fmt.Errorf("manifest must be a map or string")
	}
	path := strings.TrimSpace(getString(params, "path", ""))
	if path == "" {
		return nil, fmt.Errorf("manifest or path is required")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadUnstructuredFromBytes(payload)
}

func loadUnstructuredFromBytes(payload []byte) (*unstructured.Unstructured, error) {
	obj := map[string]interface{}{}
	if err := yaml.Unmarshal(payload, &obj); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

func mergeUnstructuredField(obj map[string]interface{}, key string, patch map[string]interface{}) {
	if obj == nil {
		return
	}
	raw, _ := obj[key]
	current, _ := raw.(map[string]interface{})
	if current == nil {
		current = make(map[string]interface{}, len(patch))
	}
	mergeMap(current, patch)
	obj[key] = current
}

func mergeMap(dst map[string]interface{}, src map[string]interface{}) {
	for key, value := range src {
		if value == nil {
			delete(dst, key)
			continue
		}
		if nested, ok := value.(map[string]interface{}); ok {
			next, _ := dst[key].(map[string]interface{})
			if next == nil {
				next = make(map[string]interface{}, len(nested))
			}
			mergeMap(next, nested)
			dst[key] = next
			continue
		}
		dst[key] = value
	}
}

func expandMapVars(input map[string]interface{}, vars map[string]string) map[string]interface{} {
	if input == nil {
		return nil
	}
	expanded := expandUnstructuredVars(input, vars)
	if mapped, ok := expanded.(map[string]interface{}); ok {
		return mapped
	}
	return input
}

func expandUnstructuredObject(obj *unstructured.Unstructured, vars map[string]string) {
	if obj == nil {
		return
	}
	if obj.Object == nil {
		return
	}
	expanded := expandUnstructuredVars(obj.Object, vars)
	if mapped, ok := expanded.(map[string]interface{}); ok {
		obj.Object = mapped
	}
}

func expandUnstructuredVars(value interface{}, vars map[string]string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[key] = expandUnstructuredVars(val, vars)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, val := range typed {
			out[i] = expandUnstructuredVars(val, vars)
		}
		return out
	case string:
		return expandVars(typed, vars)
	default:
		return value
	}
}

func getStringList(params map[string]interface{}, key string) ([]string, error) {
	if params == nil {
		return nil, nil
	}
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil, nil
		}
		parts := strings.Split(trimmed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("field %s must be a list or string", key)
	}
}
