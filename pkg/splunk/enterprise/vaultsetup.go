
package enterprise

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InjectVaultSecret represents a function that adds Vault injection annotations to the StatefulSet
// Pods deployed by the Splunk Operator.
func InjectVaultSecret(ctx context.Context, client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, vaultSpec *enterpriseApi.VaultIntegration) error {
	if !vaultSpec.Enable {
		return nil
	}

	// Validate if role and secretPath are provided
	if vaultSpec.Role == "" {
		return fmt.Errorf("vault role is required when vault is enabled")
	}
	if vaultSpec.SecretPath == "" {
		return fmt.Errorf("vault secretPath is required when vault is enabled")
	}

	secretPath := vaultSpec.SecretPath
	vaultRole := vaultSpec.Role
	secretKeyToEnv := map[string]string{
		"hec_token":    "HEC_TOKEN",
		"idxc_secret":  "IDXC_SECRET",
		"pass4SymmKey": "PASS4_SYMMKEY",
		"password":     "SPLUNK_PASSWORD",
		"shc_secret":   "SHC_SECRET",
	}

	// Adding annotations for vault injection
	annotations := map[string]string{
		"vault.hashicorp.com/agent-inject":      "true",
		"vault.hashicorp.com/agent-inject-path": "/mnt/splunk-secrets",
		"vault.hashicorp.com/role":              vaultRole,
	}

	// Adding annotations to indicate specific secrets to be injected as separate files
	// Adding annotation for default configuration file
	annotations["vault.hashicorp.com/agent-inject-file-defaults"] = "default.yml"
	annotations["vault.hashicorp.com/secret-volume-path-defaults"] = "/mnt/splunk-secrets"
    annotations["vault.hashicorp.com/agent-inject-template-defaults"] = `
splunk:
    hec_disabled: 0
    hec_enableSSL: 0
    hec_token: "{{- with secret "secret/data/splunk/hec_token" -}}{{ .Data.data.value }}{{- end }}"
    password: "{{- with secret "secret/data/splunk/password" -}}{{ .Data.data.value }}{{- end }}"
    pass4SymmKey: "{{- with secret "secret/data/splunk/pass4SymmKey" -}}{{ .Data.data.value }}{{- end }}"
    idxc:
        secret: "{{- with secret "secret/data/splunk/idxc_secret" -}}{{ .Data.data.value }}{{- end }}"
    shc:
        secret: "{{- with secret "secret/data/splunk/shc_secret" -}}{{ .Data.data.value }}{{- end }}"
`
	for key := range secretKeyToEnv {
		annotationKey := fmt.Sprintf("vault.hashicorp.com/agent-inject-secret-%s", key)
		annotations[annotationKey] = fmt.Sprintf("%s/%s", secretPath, key)
		annotationFile := fmt.Sprintf("vault.hashicorp.com/agent-inject-file-%s", key)
		annotations[annotationFile] = key
		annotationVolumeKey := fmt.Sprintf("vault.hashicorp.com/secret-volume-path-%s", key)
		annotations[annotationVolumeKey] = fmt.Sprintf("/mnt/splunk-secrets/%s", key)
	}

	// Apply these annotations to the StatefulSet PodTemplateSpec without overwriting existing ones
	if podTemplateSpec.ObjectMeta.Annotations == nil {
		podTemplateSpec.ObjectMeta.Annotations = make(map[string]string)
	}
	for key, value := range annotations {
		if existingValue, exists := podTemplateSpec.ObjectMeta.Annotations[key]; !exists || existingValue == "" {
			podTemplateSpec.ObjectMeta.Annotations[key] = value
		}
	}

	return nil
}

// AddSecretMonitorSidecarContainer adds a sidecar container to a StatefulSet to watch for changes in /mnt/splunk-secrets
func AddSecretMonitorSidecarContainer(ctx context.Context, client splcommon.ControllerClient, namespace string, podTemplateSpec *corev1.PodTemplateSpec) error {
	// Define the sidecar container
	sidecarContainer := corev1.Container{
		Name:  "file-watcher",
		Image: "busybox",
		Command: []string{
			"/bin/sh", "-c", "/mnt/scripts/sidecar_file_watcher.sh",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "splunk-secrets",
				MountPath: "/mnt/splunk-secrets",
			},
			{
				Name:      "script-volume",
				MountPath: "/mnt/scripts",
			},
		},
	}

	// Add the sidecar container to the StatefulSet's PodTemplateSpec
	podTemplateSpec.Spec.Containers = append(podTemplateSpec.Spec.Containers, sidecarContainer)

	// Define the script volume as a ConfigMap
	scriptVolume := corev1.Volume{
		Name: "script-volume",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "sidecar-script-configmap",
				},
			},
		},
	}

	// Add the script volume if it's not already present
	volumeExists := false
	for _, volume := range podTemplateSpec.Spec.Volumes {
		if volume.Name == "script-volume" {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, scriptVolume)
	}

	// Define the ConfigMap that contains the watcher script
	watcherConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sidecar-script-configmap",
			Namespace: namespace,
		},
		Data: map[string]string{
			"sidecar_file_watcher.sh": `#!/bin/bash

# This script runs as a sidecar container to monitor changes in the /mnt/splunk-secrets directory.
# If any changes are detected, the main container is restarted.

WATCH_DIR="/mnt/splunk-secrets"
SPLUNK_CONTAINER_NAME="splunk"

# Install inotify-tools if it's not present
if ! command -v inotifywait &> /dev/null; then
  apt-get update && apt-get install -y inotify-tools
fi

# Function to trigger a container restart
restart_container() {
  echo "File change detected. Restarting the Splunk container."
  kill 1 # Restarting the sidecar container will also restart the pod
}

# Watch the directory for changes
inotifywait -m -e modify,create,delete "$WATCH_DIR" |
while read path action file; do
  echo "Change detected in $WATCH_DIR: $file ($action)"
  restart_container
done`,
		},
	}

	// Apply the ConfigMap to the StatefulSet
	err := client.Create(ctx, watcherConfigMap, )
	if err != nil {
		fmt.Printf("Error creating ConfigMap: %v\n", err)
		return err
	}

	// Apply the ConfigMap to the StatefulSet's volumes
	configMapExists := false
	for _, volume := range podTemplateSpec.Spec.Volumes {
		if volume.Name == "sidecar-script-configmap" {
			configMapExists = true
			break
		}
	}
	if !configMapExists {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, scriptVolume)
	}
	return nil
}