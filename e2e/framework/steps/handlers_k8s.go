package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterK8sHandlers registers Kubernetes helper steps.
func RegisterK8sHandlers(reg *Registry) {
	reg.Register("k8s.namespace.ensure", handleNamespaceEnsure)
	reg.Register("k8s.service_account.create", handleCreateServiceAccount)
	reg.Register("assert.k8s.pod.service_account", handleAssertPodServiceAccount)
	reg.Register("k8s.resource.version.capture", handleResourceVersionCapture)
	reg.Register("k8s.resource.version.wait_change", handleResourceVersionWaitChange)
	reg.Register("k8s.resource.apply", handleResourceApply)
	reg.Register("k8s.resource.delete", handleResourceDelete)
	reg.Register("k8s.resource.patch", handleResourcePatch)
	reg.Register("k8s.configmap.update", handleConfigMapUpdate)
	reg.Register("assert.k8s.configmap.exists", handleAssertConfigMapExists)
	reg.Register("assert.k8s.configmap.contains", handleAssertConfigMapContains)
	reg.Register("assert.k8s.pod.cpu_limit", handleAssertPodCPULimit)
	reg.Register("assert.k8s.pod.resources", handleAssertPodResources)
	reg.Register("assert.k8s.pod.files.present", handleAssertPodFilesPresent)
	reg.Register("assert.k8s.pod.files.absent", handleAssertPodFilesAbsent)
	reg.Register("assert.k8s.pod.file.contains", handleAssertPodFileContains)
	reg.Register("assert.k8s.pod.env.contains", handleAssertPodEnvContains)
	reg.Register("assert.k8s.pvc.exists", handleAssertPVCExists)
	reg.Register("k8s.pod.delete", handleDeletePod)
}

func handleNamespaceEnsure(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", "")), exec.Vars)
	if namespace == "" {
		namespace = expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["namespace"])), exec.Vars)
	}
	if namespace == "" {
		prefix := "e2e"
		if exec.Config != nil && exec.Config.NamespacePrefix != "" {
			prefix = exec.Config.NamespacePrefix
		}
		namespace = fmt.Sprintf("%s-%s", prefix, topology.RandomDNSName(5))
	}
	if err := exec.Kube.EnsureNamespace(ctx, namespace); err != nil {
		return nil, err
	}
	exec.Vars["namespace"] = namespace

	baseName := expandVars(strings.TrimSpace(getString(step.With, "base_name", "")), exec.Vars)
	if baseName == "" {
		if existing := strings.TrimSpace(exec.Vars["base_name"]); existing != "" {
			baseName = existing
		} else {
			baseName = namespace
		}
	}
	exec.Vars["base_name"] = baseName
	return map[string]string{"namespace": namespace, "base_name": baseName}, nil
}

func handleCreateServiceAccount(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("service account name is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := exec.Kube.Client.Create(ctx, serviceAccount); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}
	return map[string]string{"service_account": name, "namespace": namespace}, nil
}

func handleAssertPodServiceAccount(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	expected := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if expected == "" {
		return nil, fmt.Errorf("service account name is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	podName := expandVars(strings.TrimSpace(getString(step.With, "pod", "")), exec.Vars)
	if podName == "" {
		if standalone := exec.Vars["standalone_name"]; standalone != "" {
			podName = fmt.Sprintf("splunk-%s-standalone-0", standalone)
		} else if searchPod := exec.Vars["search_pod"]; searchPod != "" {
			podName = searchPod
		}
	}
	if podName == "" {
		return nil, fmt.Errorf("pod name is required")
	}

	pod := &corev1.Pod{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, err
	}
	actual := pod.Spec.ServiceAccountName
	if actual != expected {
		return nil, fmt.Errorf("pod %s service account mismatch expected=%s actual=%s", podName, expected, actual)
	}
	return map[string]string{"pod": podName, "service_account": actual}, nil
}

func handleResourceVersionCapture(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	obj, err := getUnstructuredResource(ctx, exec, step)
	if err != nil {
		return nil, err
	}
	version := obj.GetResourceVersion()
	varKey := strings.TrimSpace(getString(step.With, "var", "last_resource_version"))
	if varKey == "" {
		varKey = "last_resource_version"
	}
	exec.Vars[varKey] = version
	return map[string]string{"resource_version": version, "var": varKey}, nil
}

func handleResourceVersionWaitChange(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	varKey := strings.TrimSpace(getString(step.With, "var", "last_resource_version"))
	prev := strings.TrimSpace(getString(step.With, "previous", exec.Vars[varKey]))
	if prev == "" {
		return nil, fmt.Errorf("previous resource version is required")
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
		obj, err := getUnstructuredResource(ctx, exec, step)
		if err != nil {
			return nil, err
		}
		current := obj.GetResourceVersion()
		if current != prev {
			exec.Vars[varKey] = current
			return map[string]string{"resource_version": current, "previous": prev}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("resource version did not change within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func getUnstructuredResource(ctx context.Context, exec *Context, step spec.StepSpec) (*unstructured.Unstructured, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	kind := expandVars(strings.TrimSpace(getString(step.With, "kind", "")), exec.Vars)
	if kind == "" {
		return nil, fmt.Errorf("kind is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	apiVersion := expandVars(strings.TrimSpace(getString(step.With, "apiVersion", "enterprise.splunk.com/v4")), exec.Vars)
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
