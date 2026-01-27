package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/url"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterLicenseHandlers registers license-related steps.
func RegisterLicenseHandlers(reg *Registry) {
	reg.Register("license.configmap.ensure", handleLicenseConfigMapEnsure)
	reg.Register("splunk.license_manager.deploy", handleLicenseManagerDeploy)
	reg.Register("splunk.license_manager.wait_ready", handleLicenseManagerWaitReady)
	reg.Register("splunk.license_master.deploy", handleLicenseMasterDeploy)
	reg.Register("splunk.license_master.wait_ready", handleLicenseMasterWaitReady)
	reg.Register("splunk.monitoring_console.deploy", handleMonitoringConsoleDeploy)
	reg.Register("splunk.monitoring_console.wait_ready", handleMonitoringConsoleWaitReady)
	reg.Register("splunk.license_manager.verify_configured", handleLicenseManagerVerifyConfigured)
}

func handleLicenseConfigMapEnsure(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	path := expandVars(getString(step.With, "path", ""), exec.Vars)
	if path == "" {
		path = os.Getenv("E2E_LICENSE_FILE")
	}
	if path == "" {
		return nil, fmt.Errorf("license file path is required")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if err := exec.Kube.EnsureNamespace(ctx, namespace); err != nil {
		return nil, err
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", "")), exec.Vars)
	if name == "" {
		name = namespace
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(getString(step.With, "key", ""))
	if key == "" {
		key = filepath.Base(path)
	}
	aliasKey := "enterprise.lic"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: string(data),
		},
	}
	if key != aliasKey {
		cm.Data[aliasKey] = string(data)
	}

	if err := exec.Kube.Client.Create(ctx, cm); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		existing := &corev1.ConfigMap{}
		if err := exec.Kube.Client.Get(ctx, clientKey(namespace, name), existing); err != nil {
			return nil, err
		}
		existing.Data = cm.Data
		if err := exec.Kube.Client.Update(ctx, existing); err != nil {
			return nil, err
		}
	}
	exec.Vars["license_configmap"] = name
	exec.Vars["license_key"] = aliasKey
	if key == aliasKey {
		exec.Vars["license_key"] = key
	}
	return map[string]string{"name": name, "namespace": namespace, "key": key}, nil
}

func handleLicenseManagerDeploy(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["base_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("license manager name is required")
	}
	if err := exec.Kube.EnsureNamespace(ctx, namespace); err != nil {
		return nil, err
	}
	licenseConfig := expandVars(strings.TrimSpace(getString(step.With, "configmap", exec.Vars["license_configmap"])), exec.Vars)
	if licenseConfig == "" {
		return nil, fmt.Errorf("license configmap is required")
	}
	_, err := topology.DeployLicenseManager(ctx, exec.Kube, namespace, name, exec.Config.SplunkImage, licenseConfig)
	if err != nil {
		return nil, err
	}
	exec.Vars["license_manager_name"] = name
	return map[string]string{"name": name}, nil
}

func handleLicenseManagerWaitReady(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["license_manager_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("license manager name is required")
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	if err := topology.WaitLicenseManagerReady(ctx, exec.Kube, namespace, name, timeout); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "status": "ready"}, nil
}

func handleLicenseMasterDeploy(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["base_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("license master name is required")
	}
	if err := exec.Kube.EnsureNamespace(ctx, namespace); err != nil {
		return nil, err
	}
	licenseConfig := expandVars(strings.TrimSpace(getString(step.With, "configmap", exec.Vars["license_configmap"])), exec.Vars)
	if licenseConfig == "" {
		return nil, fmt.Errorf("license configmap is required")
	}
	_, err := topology.DeployLicenseMaster(ctx, exec.Kube, namespace, name, exec.Config.SplunkImage, licenseConfig)
	if err != nil {
		return nil, err
	}
	exec.Vars["license_master_name"] = name
	return map[string]string{"name": name}, nil
}

func handleLicenseMasterWaitReady(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["license_master_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("license master name is required")
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	if err := topology.WaitLicenseMasterReady(ctx, exec.Kube, namespace, name, timeout); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "status": "ready"}, nil
}

func handleMonitoringConsoleDeploy(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["base_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("monitoring console name is required")
	}
	licenseManagerRef := expandVars(strings.TrimSpace(getString(step.With, "license_manager_ref", exec.Vars["license_manager_name"])), exec.Vars)
	licenseMasterRef := expandVars(strings.TrimSpace(getString(step.With, "license_master_ref", exec.Vars["license_master_name"])), exec.Vars)
	_, err := topology.DeployMonitoringConsole(ctx, exec.Kube, namespace, name, exec.Config.SplunkImage, licenseManagerRef, licenseMasterRef)
	if err != nil {
		return nil, err
	}
	exec.Vars["monitoring_console_name"] = name
	return map[string]string{"name": name}, nil
}

func handleMonitoringConsoleWaitReady(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	name := expandVars(strings.TrimSpace(getString(step.With, "name", exec.Vars["monitoring_console_name"])), exec.Vars)
	if name == "" {
		return nil, fmt.Errorf("monitoring console name is required")
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	if err := topology.WaitMonitoringConsoleReady(ctx, exec.Kube, namespace, name, timeout); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "status": "ready"}, nil
}

func handleLicenseManagerVerifyConfigured(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
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
	expected := expandVars(strings.TrimSpace(getString(step.With, "expected_contains", "license-manager-service:8089")), exec.Vars)
	if expected == "" {
		return nil, fmt.Errorf("expected_contains is required")
	}

	// Get retry configuration
	retries := getInt(step.With, "retries", 30)
	retryInterval := getDuration(step.With, "retry_interval", 2*time.Second)

	// Retry logic: license configuration may take time to propagate
	for _, pod := range pods {
		client := exec.Splunkd.WithPod(pod)
		var lastErr error
		for attempt := 0; attempt <= retries; attempt++ {
			payload, err := client.ManagementRequest(ctx, "GET", "/services/licenser/localslave", url.Values{"output_mode": []string{"json"}}, nil)
			if err != nil {
				lastErr = err
				if attempt < retries {
					time.Sleep(retryInterval)
					continue
				}
				return nil, fmt.Errorf("failed to check license on pod %s after %d retries: %w", pod, retries, err)
			}
			if strings.Contains(string(payload), expected) {
				// Success
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("license manager not configured on pod %s (expected: %s)", pod, expected)
			if attempt < retries {
				time.Sleep(retryInterval)
				continue
			}
		}
		if lastErr != nil {
			return nil, lastErr
		}
	}
	return map[string]string{"pods": strings.Join(pods, ","), "expected": expected}, nil
}

func clientKey(namespace, name string) client.ObjectKey {
	return client.ObjectKey{Namespace: namespace, Name: name}
}
