package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/splunkd"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterSecretHandlers registers secret-related steps.
func RegisterSecretHandlers(reg *Registry) {
	reg.Register("secret.capture", handleSecretCapture)
	reg.Register("secret.generate", handleSecretGenerate)
	reg.Register("secret.update", handleSecretUpdate)
	reg.Register("secret.delete", handleSecretDelete)
	reg.Register("secret.versioned.list", handleSecretVersionedList)
	reg.Register("secret.verify.objects", handleSecretVerifyObjects)
	reg.Register("secret.verify.pods", handleSecretVerifyPods)
	reg.Register("secret.verify.server_conf", handleSecretVerifyServerConf)
	reg.Register("secret.verify.inputs_conf", handleSecretVerifyInputsConf)
	reg.Register("secret.verify.api", handleSecretVerifyAPI)
}

func handleSecretCapture(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	secretName := secretObjectName(step, exec, namespace)
	secret := &corev1.Secret{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret); err != nil {
		return nil, err
	}
	data := make(map[string]string, len(secret.Data))
	for key, value := range secret.Data {
		data[key] = string(value)
	}
	path, err := writeSecretDataArtifact(exec, data, "secret-capture")
	if err != nil {
		return nil, err
	}
	varKey := strings.TrimSpace(getString(step.With, "var", "last_secret_data_path"))
	if varKey == "" {
		varKey = "last_secret_data_path"
	}
	exec.Vars[varKey] = path
	exec.Vars["last_secret_data_path"] = path
	exec.Vars["last_secret_name"] = secretName
	exec.Vars["secret_name"] = secretName
	return map[string]string{"name": secretName, "path": path, "var": varKey}, nil
}

func handleSecretGenerate(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	data := map[string]string{}
	if !getBool(step.With, "empty", false) {
		data = map[string]string{
			"hec_token":    randomHECToken(),
			"password":     topology.RandomDNSName(12),
			"pass4SymmKey": topology.RandomDNSName(12),
			"idxc_secret":  topology.RandomDNSName(12),
			"shc_secret":   topology.RandomDNSName(12),
		}
		if override := getString(step.With, "password", ""); override != "" {
			data["password"] = override
		}
	}
	path, err := writeSecretDataArtifact(exec, data, "secret-generate")
	if err != nil {
		return nil, err
	}
	varKey := strings.TrimSpace(getString(step.With, "var", "last_secret_data_path"))
	if varKey == "" {
		varKey = "last_secret_data_path"
	}
	exec.Vars[varKey] = path
	exec.Vars["last_secret_data_path"] = path
	return map[string]string{"path": path, "var": varKey}, nil
}

func handleSecretUpdate(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	secretName := secretObjectName(step, exec, namespace)
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: namespace, Name: secretName}
	if err := exec.Kube.Client.Get(ctx, key, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if err := exec.Kube.Client.Create(ctx, secret); err != nil {
			return nil, err
		}
	} else {
		secret.Data = data
		secret.Type = corev1.SecretTypeOpaque
		if err := exec.Kube.Client.Update(ctx, secret); err != nil {
			return nil, err
		}
	}
	if exec != nil {
		exec.Vars["secret_name"] = secretName
	}
	return map[string]string{"name": secretName}, nil
}

func handleSecretDelete(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	secretName := secretObjectName(step, exec, namespace)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace}}
	if err := exec.Kube.Client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	return map[string]string{"name": secretName, "deleted": "true"}, nil
}

func handleSecretVersionedList(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	version := getInt(step.With, "version", 2)
	list := &corev1.SecretList{}
	if err := exec.Kube.Client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	suffix := fmt.Sprintf("v%d", version)
	names := make([]string, 0)
	for _, item := range list.Items {
		name := item.Name
		if strings.HasPrefix(name, "splunk") && strings.HasSuffix(name, suffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	exec.Vars["last_secret_names"] = strings.Join(names, ",")
	return map[string]string{"count": fmt.Sprintf("%d", len(names)), "names": strings.Join(names, ",")}, nil
}

func handleSecretVerifyObjects(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	namespace := strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"]))
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	names := expandVars(getString(step.With, "names", exec.Vars["last_secret_names"]), exec.Vars)
	if names == "" {
		return nil, fmt.Errorf("secret names are required")
	}
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	match := getBool(step.With, "match", true)
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}
	for _, name := range splitNames(names) {
		current := &corev1.Secret{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, current); err != nil {
			return nil, err
		}
		if err := compareSecretData(data, current.Data, match); err != nil {
			return nil, fmt.Errorf("secret %s: %w", name, err)
		}
	}
	return map[string]string{"verified": "true"}, nil
}

func handleSecretVerifyPods(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	pods, err := listSplunkPods(ctx, exec)
	if err != nil {
		return nil, err
	}
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	match := getBool(step.With, "match", true)
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		for key, value := range data {
			current, err := readMountedKey(ctx, exec, pod, key)
			if err != nil {
				return nil, err
			}
			if (current == string(value)) != match {
				return nil, fmt.Errorf("pod %s key %s match=%t", pod, key, match)
			}
		}
	}
	return map[string]string{"verified": "true", "pods": strings.Join(pods, ",")}, nil
}

func handleSecretVerifyServerConf(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	pods, err := listSplunkPods(ctx, exec)
	if err != nil {
		return nil, err
	}
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	match := getBool(step.With, "match", true)
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		keys := secretKeysForPod(pod)
		for _, key := range keys {
			stanza := secretKeyToServerConfStanza[key]
			value, err := readSecretFromServerConf(ctx, exec, pod, "pass4SymmKey", stanza)
			if err != nil {
				return nil, err
			}
			if (value == string(data[key])) != match {
				return nil, fmt.Errorf("pod %s server.conf %s match=%t", pod, key, match)
			}
		}
	}
	return map[string]string{"verified": "true", "pods": strings.Join(pods, ",")}, nil
}

func handleSecretVerifyInputsConf(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	pods, err := listSplunkPods(ctx, exec)
	if err != nil {
		return nil, err
	}
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	match := getBool(step.With, "match", true)
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		if !strings.Contains(pod, "standalone") && !strings.Contains(pod, "indexer") {
			continue
		}
		value, err := readSecretFromInputsConf(ctx, exec, pod, "token", secretKeyToServerConfStanza["hec_token"])
		if err != nil {
			return nil, err
		}
		if (value == string(data["hec_token"])) != match {
			return nil, fmt.Errorf("pod %s inputs.conf hec_token match=%t", pod, match)
		}
	}
	return map[string]string{"verified": "true", "pods": strings.Join(pods, ",")}, nil
}

func handleSecretVerifyAPI(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	pods, err := listSplunkPods(ctx, exec)
	if err != nil {
		return nil, err
	}
	dataPath := expandVars(getString(step.With, "data_path", exec.Vars["last_secret_data_path"]), exec.Vars)
	if dataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}
	match := getBool(step.With, "match", true)
	data, err := readSecretData(dataPath)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		keys := []string{"password"}
		if strings.Contains(pod, "standalone") || strings.Contains(pod, "indexer") {
			keys = []string{"password", "hec_token"}
		}
		for _, key := range keys {
			ok, err := checkSecretViaAPI(ctx, exec, pod, key, string(data[key]))
			if err != nil {
				return nil, err
			}
			if ok != match {
				return nil, fmt.Errorf("pod %s api key %s match=%t", pod, key, match)
			}
		}
	}
	return map[string]string{"verified": "true", "pods": strings.Join(pods, ",")}, nil
}

func secretObjectName(step spec.StepSpec, exec *Context, namespace string) string {
	name := strings.TrimSpace(getString(step.With, "name", ""))
	if name != "" {
		return name
	}
	if exec != nil {
		if value := strings.TrimSpace(exec.Vars["secret_name"]); value != "" {
			return value
		}
	}
	return fmt.Sprintf("splunk-%s-secret", namespace)
}

func writeSecretDataArtifact(exec *Context, data map[string]string, prefix string) (string, error) {
	artifactName := fmt.Sprintf("%s-%s.json", prefix, sanitize(exec.TestName))
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return exec.Artifacts.WriteText(artifactName, string(payload))
}

func readSecretData(path string) (map[string][]byte, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := map[string]string{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(raw))
	for key, value := range raw {
		out[key] = []byte(value)
	}
	return out, nil
}

func splitNames(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func compareSecretData(expected, actual map[string][]byte, match bool) error {
	for key, value := range expected {
		actualValue := actual[key]
		equal := string(actualValue) == string(value)
		if equal != match {
			return fmt.Errorf("secret key %s match=%t", key, match)
		}
	}
	return nil
}

func listSplunkPods(ctx context.Context, exec *Context) ([]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(exec.Vars["namespace"])
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	pods, err := exec.Kube.ListPods(ctx, namespace)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, pod := range pods {
		if strings.HasPrefix(pod.Name, "splunk") && !strings.HasPrefix(pod.Name, "splunk-op") {
			names = append(names, pod.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func readMountedKey(ctx context.Context, exec *Context, podName, key string) (string, error) {
	stdout, _, err := exec.Kube.Exec(ctx, exec.Vars["namespace"], podName, "", []string{"cat", fmt.Sprintf("/mnt/splunk-secrets/%s", key)}, "", false)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func readSecretFromServerConf(ctx context.Context, exec *Context, podName, key, stanza string) (string, error) {
	line, err := getConfLineFromPod(ctx, exec, podName, "/opt/splunk/etc/system/local/server.conf", key, stanza, true)
	if err != nil {
		return "", err
	}
	parts := strings.Split(line, "=")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid config line: %s", line)
	}
	secretValue := strings.TrimSpace(parts[1])
	return decryptSplunkSecret(ctx, exec, podName, secretValue), nil
}

func readSecretFromInputsConf(ctx context.Context, exec *Context, podName, key, stanza string) (string, error) {
	line, err := getConfLineFromPod(ctx, exec, podName, "/opt/splunk/etc/apps/splunk_httpinput/local/inputs.conf", key, stanza, true)
	if err != nil {
		return "", err
	}
	parts := strings.Split(line, "=")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid config line: %s", line)
	}
	return strings.TrimSpace(parts[1]), nil
}

func decryptSplunkSecret(ctx context.Context, exec *Context, podName, secretValue string) string {
	stdout, _, err := exec.Kube.Exec(ctx, exec.Vars["namespace"], podName, "", []string{"/opt/splunk/bin/splunk", "show-decrypted", "--value", secretValue}, "", false)
	if err != nil {
		return "Failed"
	}
	return strings.TrimSpace(stdout)
}

func getConfLineFromPod(ctx context.Context, exec *Context, podName, filePath, configName, stanza string, checkStanza bool) (string, error) {
	stdout, _, err := exec.Kube.Exec(ctx, exec.Vars["namespace"], podName, "", []string{"cat", filePath}, "", false)
	if err != nil {
		return "", err
	}
	lines := strings.Split(stdout, "\n")
	stanzaFound := !checkStanza
	targetStanza := fmt.Sprintf("[%s]", stanza)
	for _, line := range lines {
		if line == "" {
			continue
		}
		if !stanzaFound {
			if strings.HasPrefix(line, targetStanza) {
				stanzaFound = true
			}
			continue
		}
		if strings.HasPrefix(line, configName) {
			return line, nil
		}
	}
	return "", fmt.Errorf("config %s not found under stanza %s", configName, stanza)
}

func checkSecretViaAPI(ctx context.Context, exec *Context, podName, key, value string) (bool, error) {
	if exec == nil || exec.Kube == nil {
		return false, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(exec.Vars["namespace"])
	if namespace == "" {
		return false, fmt.Errorf("namespace not set")
	}
	client := splunkd.NewClient(exec.Kube, namespace, podName)
	if secretName := strings.TrimSpace(exec.Vars["secret_name"]); secretName != "" {
		client = client.WithSecretName(secretName)
	}
	switch key {
	case "password":
		if err := client.CheckCredentials(ctx, "admin", value); err != nil {
			return false, err
		}
		return true, nil
	case "hec_token":
		if err := client.SendHECEvent(ctx, value, "data"); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported secret key: %s", key)
	}
}

func secretKeysForPod(podName string) []string {
	if strings.Contains(podName, "standalone") || strings.Contains(podName, "license-manager") || strings.Contains(podName, "monitoring-console") {
		return []string{"pass4SymmKey"}
	}
	if strings.Contains(podName, "indexer") || strings.Contains(podName, "cluster-manager") {
		return []string{"pass4SymmKey", "idxc_secret"}
	}
	if strings.Contains(podName, "search-head") || strings.Contains(podName, "-deployer-") {
		return []string{"pass4SymmKey", "shc_secret"}
	}
	return []string{"pass4SymmKey"}
}

func randomHECToken() string {
	parts := []string{
		strings.ToUpper(topology.RandomDNSName(8)),
		strings.ToUpper(topology.RandomDNSName(4)),
		strings.ToUpper(topology.RandomDNSName(4)),
		strings.ToUpper(topology.RandomDNSName(4)),
		strings.ToUpper(topology.RandomDNSName(12)),
	}
	return strings.Join(parts, "-")
}

var secretKeyToServerConfStanza = map[string]string{
	"shc_secret":   "shclustering",
	"idxc_secret":  "clustering",
	"pass4SymmKey": "general",
	"hec_token":    "http://splunk_hec_token",
}
