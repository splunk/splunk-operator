package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/results"
	corev1 "k8s.io/api/core/v1"
)

func (r *Runner) shouldCollectLogs(status results.Status) bool {
	switch strings.ToLower(strings.TrimSpace(r.cfg.LogCollection)) {
	case "always":
		return true
	case "never":
		return false
	default:
		return status == results.StatusFailed
	}
}

func (r *Runner) collectLogsForTest(ctx context.Context, namespace string, result *results.TestResult) {
	if namespace == "" || !r.shouldCollectLogs(result.Status) {
		return
	}

	collectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	path, err := r.ensureNamespaceLogs(collectCtx, namespace)
	if err != nil {
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["log_collection_error"] = err.Error()
	} else {
		if result.Artifacts == nil {
			result.Artifacts = make(map[string]string)
		}
		result.Artifacts["logs"] = path
	}

	if r.cfg.OperatorNamespace != "" && r.cfg.OperatorNamespace != namespace {
		opPath, opErr := r.ensureNamespaceLogs(collectCtx, r.cfg.OperatorNamespace)
		if opErr != nil {
			if result.Metadata == nil {
				result.Metadata = make(map[string]string)
			}
			result.Metadata["operator_log_error"] = opErr.Error()
		} else {
			if result.Artifacts == nil {
				result.Artifacts = make(map[string]string)
			}
			result.Artifacts["operator_logs"] = opPath
		}
	}
}

func (r *Runner) ensureNamespaceLogs(ctx context.Context, namespace string) (string, error) {
	key := strings.TrimSpace(namespace)
	if key == "" {
		return "", fmt.Errorf("namespace is required")
	}
	r.logMu.Lock()
	if path, ok := r.logCollected[key]; ok {
		r.logMu.Unlock()
		return path, nil
	}
	r.logMu.Unlock()

	path := filepath.Join(r.artifacts.RunDir, "logs", key)
	if err := r.collectNamespaceLogs(ctx, namespace, path); err != nil {
		return path, err
	}

	r.logMu.Lock()
	r.logCollected[key] = path
	r.logMu.Unlock()
	return path, nil
}

func (r *Runner) collectNamespaceLogs(ctx context.Context, namespace, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	errs := make([]string, 0)
	pods, err := r.kube.ListPods(ctx, namespace)
	if err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(targetDir, "pods.json"), pods); err != nil {
		errs = append(errs, fmt.Sprintf("pods.json: %v", err))
	}

	events, err := r.kube.ListEvents(ctx, namespace)
	if err != nil {
		errs = append(errs, fmt.Sprintf("events: %v", err))
	} else if err := writeJSON(filepath.Join(targetDir, "events.json"), events); err != nil {
		errs = append(errs, fmt.Sprintf("events.json: %v", err))
	}

	for _, pod := range pods {
		podDir := filepath.Join(targetDir, "pods", pod.Name)
		if err := os.MkdirAll(podDir, 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("pod %s dir: %v", pod.Name, err))
			continue
		}
		if err := writeJSON(filepath.Join(podDir, "pod.json"), pod); err != nil {
			errs = append(errs, fmt.Sprintf("pod %s json: %v", pod.Name, err))
		}

		for _, container := range podContainers(pod) {
			logs, logErr := r.kube.GetPodLogs(ctx, namespace, pod.Name, container, false)
			if logErr != nil {
				errs = append(errs, fmt.Sprintf("pod %s container %s logs: %v", pod.Name, container, logErr))
			} else if logs != "" {
				if err := writeText(filepath.Join(podDir, fmt.Sprintf("%s.log", sanitizeName(container))), logs); err != nil {
					errs = append(errs, fmt.Sprintf("pod %s container %s write: %v", pod.Name, container, err))
				}
			}

			if isSplunkContainer(pod, container) {
				if err := r.collectSplunkInternalLogs(ctx, namespace, pod.Name, container, podDir); err != nil {
					errs = append(errs, fmt.Sprintf("pod %s container %s splunk logs: %v", pod.Name, container, err))
				}
			}
		}
	}

	if len(errs) > 0 {
		_ = writeText(filepath.Join(targetDir, "errors.log"), strings.Join(errs, "\n"))
		return fmt.Errorf("log collection completed with errors")
	}
	return nil
}

func (r *Runner) collectSplunkInternalLogs(ctx context.Context, namespace, podName, container, podDir string) error {
	logFiles := []string{
		"/opt/splunk/var/log/splunk/splunkd.log",
		"/opt/splunk/var/log/splunk/metrics.log",
		"/opt/splunk/var/log/splunk/search.log",
		"/opt/splunk/var/log/splunk/scheduler.log",
		"/opt/splunk/var/log/splunk/audit.log",
	}

	logDir := filepath.Join(podDir, "splunk")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	tail := r.cfg.SplunkLogTail
	for _, path := range logFiles {
		cmd := buildLogReadCommand(path, tail)
		stdout, _, err := r.kube.Exec(ctx, namespace, podName, container, []string{"sh", "-c", cmd}, "", false)
		if err != nil || strings.TrimSpace(stdout) == "" {
			continue
		}
		name := fmt.Sprintf("%s-%s", sanitizeName(container), sanitizeName(filepath.Base(path)))
		if err := writeText(filepath.Join(logDir, name), stdout); err != nil {
			return err
		}
	}
	return nil
}

func buildLogReadCommand(path string, tail int) string {
	if tail > 0 {
		return fmt.Sprintf("if [ -f %s ]; then tail -n %d %s; fi", path, tail, path)
	}
	return fmt.Sprintf("if [ -f %s ]; then cat %s; fi", path, path)
}

func podContainers(pod corev1.Pod) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, container := range pod.Spec.InitContainers {
		if container.Name == "" || seen[container.Name] {
			continue
		}
		seen[container.Name] = true
		out = append(out, container.Name)
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == "" || seen[container.Name] {
			continue
		}
		seen[container.Name] = true
		out = append(out, container.Name)
	}
	return out
}

func isSplunkContainer(pod corev1.Pod, containerName string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}
		name := strings.ToLower(container.Name)
		image := strings.ToLower(container.Image)
		if strings.Contains(name, "splunk") || strings.Contains(image, "splunk") {
			return true
		}
	}
	return false
}

func sanitizeName(value string) string {
	clean := strings.ToLower(value)
	clean = strings.ReplaceAll(clean, " ", "-")
	clean = strings.ReplaceAll(clean, "/", "-")
	clean = strings.ReplaceAll(clean, ":", "-")
	if clean == "" {
		return "unknown"
	}
	return clean
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func writeText(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0o644)
}
