// Copyright (c) 2018-2025 Splunk Inc.
// SPDX-License-Identifier: Apache-2.0

package enterprise

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
)

// -------------------------------
// Role Defaults: names & content
// -------------------------------

// Well-known names so customers can find/manage them easily.
func GetRoleDefaultsConfigMapName(t InstanceType) string {
	switch t {
	case SplunkStandalone:
		return "defaults-base-standalone"
	case SplunkClusterManager, SplunkClusterMaster:
		return "defaults-base-clustermanager"
	case SplunkIndexer:
		return "defaults-base-idx"
	case SplunkSearchHead:
		return "defaults-base-sh"
	case SplunkMonitoringConsole:
		return "defaults-base-mc"
	case SplunkLicenseManager, SplunkLicenseMaster:
		return "defaults-base-lm"
	default:
		return "defaults-base-generic"
	}
}

// SHA256 for seed tracking (for non-destructive upgrades when customers didn't edit).
func seedHash(b string) string {
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}

// Minimal, safe role defaults matching docker-splunk / Splunk-Ansible structure.
func builtinRoleDefaultsYAML(t InstanceType) string {
	switch t {
	case SplunkStandalone:
		return baseStandalone
	case SplunkClusterManager, SplunkClusterMaster:
		return baseClusterManager
	case SplunkIndexer:
		return baseIndexer
	case SplunkSearchHead:
		return baseSearchHead
	case SplunkMonitoringConsole:
		return baseMonitoringConsole
	case SplunkLicenseManager, SplunkLicenseMaster:
		return baseLicense
	default:
		return baseGeneric
	}
}

// -------------------------------------------------------
// EnsureRoleDefaultsConfigMap: create, or safe seed-upgrade
// -------------------------------------------------------

func EnsureRoleDefaultsConfigMap(ctx context.Context, client splcommon.ControllerClient, ns string, t InstanceType) (*corev1.ConfigMap, error) {
	name := GetRoleDefaultsConfigMapName(t)
	nn := types.NamespacedName{Namespace: ns, Name: name}

	cm, err := splctrl.GetConfigMap(ctx, client, nn)
	builtin := builtinRoleDefaultsYAML(t)
	newHash := seedHash(builtin)

	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, err
		}
		// Create with our seed (managed-by + seed-hash); customers can edit later.
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "splunk-operator",
				},
				Annotations: map[string]string{
					"enterprise.splunk.com/seed-hash": newHash,
				},
			},
			Data: map[string]string{
				"defaults.yaml": builtin,
			},
		}
		_, err = splctrl.ApplyConfigMap(ctx, client, cm)
		return cm, err
	}

	// Exists. Only refresh if we created it previously AND content is still the old seed.
	lbl := cm.Labels["app.kubernetes.io/managed-by"]
	oldHash := cm.Annotations["enterprise.splunk.com/seed-hash"]
	current := cm.Data["defaults.yaml"]

	if lbl == "splunk-operator" && oldHash == seedHash(current) && oldHash != newHash {
		if cm.Annotations == nil {
			cm.Annotations = map[string]string{}
		}
		cm.Data["defaults.yaml"] = builtin
		cm.Annotations["enterprise.splunk.com/seed-hash"] = newHash
		_, err = splctrl.ApplyConfigMap(ctx, client, cm)
		if err != nil {
			return cm, err
		}
	}

	return cm, nil
}

// ------------------------------------------------------------
// Idempotent mount & chaining helpers
// ------------------------------------------------------------

// hasVolume returns true if a volume with name exists in the pod template.
func hasVolume(pod *corev1.PodTemplateSpec, name string) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

// hasMount returns true if the container already mounts volume name at path.
func hasMount(c *corev1.Container, name, path string) bool {
	for _, m := range c.VolumeMounts {
		if m.Name == name && m.MountPath == path {
			return true
		}
	}
	return false
}

// upsertEnv adds or updates the given env var on the container.
func upsertEnv(c *corev1.Container, key, val string) {
	for i := range c.Env {
		if c.Env[i].Name == key {
			c.Env[i].Value = val
			return
		}
	}
	c.Env = append(c.Env, corev1.EnvVar{Name: key, Value: val})
}

// getContainerIndexByName finds the index of a container by name.
func getContainerIndexByName(pod *corev1.PodTemplateSpec, name string) (int, bool) {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return i, true
		}
	}
	return -1, false
}

// addVolumeToTemplateIdempotent adds a ConfigMap-backed volume + mount to ALL containers, idempotently.
// (We keep the signature general; callers can reuse for other files too.)
func addVolumeToTemplateIdempotent(
	pod *corev1.PodTemplateSpec,
	volName, mountPath string,
	cmName, key, fileName string,
	mode int32,
) {
	if !hasVolume(pod, volName) {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
					DefaultMode:          &mode,
					Items:                []corev1.KeyToPath{{Key: key, Path: fileName, Mode: &mode}},
				},
			},
		})
	}

	for i := range pod.Spec.Containers {
		if !hasMount(&pod.Spec.Containers[i], volName, mountPath) {
			pod.Spec.Containers[i].VolumeMounts = append(
				pod.Spec.Containers[i].VolumeMounts,
				corev1.VolumeMount{Name: volName, MountPath: mountPath},
			)
		}
	}
}

// AddRoleDefaultsToPodTemplate mounts the role defaults CM at /mnt/role-defaults
// and annotates the *PodTemplateSpec* so edits recycle pods.
// Returns the full path to /mnt/role-defaults/defaults.yaml for convenience.
func AddRoleDefaultsToPodTemplate(
	ctx context.Context,
	client splcommon.ControllerClient,
	podTemplate *corev1.PodTemplateSpec,
	cr splcommon.MetaObject,
	t InstanceType,
) (mountedPath string, cm *corev1.ConfigMap, err error) {

	cm, err = EnsureRoleDefaultsConfigMap(ctx, client, cr.GetNamespace(), t)
	if err != nil {
		return "", nil, err
	}

	if podTemplate.ObjectMeta.Annotations == nil {
		podTemplate.ObjectMeta.Annotations = map[string]string{}
	}

	const (
		volName  = "mnt-role-defaults"
		mountDir = "/mnt/role-defaults"
		key      = "defaults.yaml"
		fileName = "defaults.yaml"
	)
	mode := int32(corev1.ConfigMapVolumeSourceDefaultMode)

	// Idempotent add (no duplicate volumes or mounts on repeated calls)
	addVolumeToTemplateIdempotent(podTemplate, volName, mountDir, cm.GetName(), key, fileName, mode)

	// Change annotation when ConfigMap resourceVersion changes to trigger rollout
	podTemplate.ObjectMeta.Annotations["roleDefaultsRev"] = cm.ResourceVersion

	return fmt.Sprintf("%s/%s", mountDir, fileName), cm, nil
}

// WireRoleDefaultsIntoEnv prepends the mounted role defaults file to SPLUNK_DEFAULTS_URL
// on the "splunk" container (creating the env var if needed).
func WireRoleDefaultsIntoEnv(podTemplate *corev1.PodTemplateSpec, defaultsFilePath string) {
	idx, ok := getContainerIndexByName(podTemplate, "splunk")
	if !ok {
		// If the "splunk" container is not present, apply to the first container.
		if len(podTemplate.Spec.Containers) == 0 {
			return
		}
		idx = 0
	}

	// Fetch current value (if any) then prepend ours (operator precedence).
	cur := ""
	for _, e := range podTemplate.Spec.Containers[idx].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			cur = e.Value
			break
		}
	}
	newVal := PrependDefaultsURL(cur, defaultsFilePath)
	upsertEnv(&podTemplate.Spec.Containers[idx], "SPLUNK_DEFAULTS_URL", newVal)
}

// EnsureRoleDefaultsAndWire is a convenience wrapper that:
// 1) Ensures the role defaults ConfigMap exists (or seed-upgrades it),
// 2) Mounts it into the PodTemplateSpec,
// 3) Prepends the defaults file into SPLUNK_DEFAULTS_URL on the "splunk" container.
func EnsureRoleDefaultsAndWire(
	ctx context.Context,
	client splcommon.ControllerClient,
	podTemplate *corev1.PodTemplateSpec,
	cr splcommon.MetaObject,
	t InstanceType,
) (*corev1.ConfigMap, error) {

	path, cm, err := AddRoleDefaultsToPodTemplate(ctx, client, podTemplate, cr, t)
	if err != nil {
		return nil, err
	}
	WireRoleDefaultsIntoEnv(podTemplate, path)
	return cm, nil
}

// PrependDefaultsURL mirrors operator behavior: put "extra" on the left.
func PrependDefaultsURL(existing, extra string) string {
	if strings.TrimSpace(extra) == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	return extra + "," + existing
}

// --------------------------
// Built-in YAML (safe base)
// --------------------------

const baseCommon = `
hide_password: true
ansible_pre_tasks: ""
splunk:
  http_enableSSL: 1
  http_enableSSL_cert: /opt/splunk/etc/auth/tls.crt
  http_enableSSL_privKey: /opt/splunk/etc/auth/tls.key
  launch:
    PYTHONHTTPSVERIFY: 1
    SPLUNK_FIPS: 1
`

// Standalone
const baseStandalone = baseCommon + `
splunk:
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
          kvstore:
            disabled: "true"
    - key: inputs
      value:
        directory: /opt/splunk/etc/system/local
        content:
          http:
            disabled: 1
    - key: web
      value:
        directory: /opt/splunk/etc/system/local
        content:
          settings:
            ui_inactivity_timeout: 5
            tools.sessions.timeout: 5
`

// Cluster Manager / Master
const baseClusterManager = baseCommon + `
splunk:
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
          kvstore:
            disabled: "true"
    - key: inputs
      value:
        directory: /opt/splunk/etc/system/local
        content:
          http:
            disabled: 1
    - key: web
      value:
        directory: /opt/splunk/etc/system/local
        content:
          settings:
            ui_inactivity_timeout: 5
            tools.sessions.timeout: 5
    - key: indexes
      value:
        directory: /opt/splunk/etc/manager-apps/_cluster/local
        content:
          _introspection:
            repFactor: auto
          _metrics:
            repFactor: auto
          _metrics_rollup:
            repFactor: auto
          _configtracker:
            repFactor: auto
`

// Indexer
const baseIndexer = baseCommon + `
splunk:
  s2s:
    ca: /opt/splunk/etc/auth/ca.crt
    cert: /opt/splunk/etc/auth/splunk-bundle.crt
    enable: "true"
    port: 9997
    ssl: "true"
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
          kvstore:
            disabled: "true"
          replication_port://9887:
            disabled: "true"
          replication_port-ssl://9887:
            serverCert: /opt/splunk/etc/auth/splunk-bundle.crt
            requireClientCert: "true"
    - key: inputs
      value:
        directory: /opt/splunk/etc/system/local
        content:
          http:
            disabled: 1
          SSL:
            requireClientCert: "true"
    - key: web
      value:
        directory: /opt/splunk/etc/system/local
        content:
          settings:
            startwebserver: 0
`

// Search Head
const baseSearchHead = baseCommon + `
splunk:
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
          kvstore:
            sslVerifyServerCert: "true"
          replication_port://9887:
            disabled: "true"
          replication_port-ssl://9887:
            serverCert: /opt/splunk/etc/auth/splunk-bundle.crt
            requireClientCert: "true"
          httpServer:
            max_content_length: 5000000000
    - key: web
      value:
        directory: /opt/splunk/etc/system/local
        content:
          settings:
            ui_inactivity_timeout: 5
            tools.sessions.timeout: 5
            max_upload_size: 2048
            splunkdConnectionTimeout: 300
`

// Monitoring Console
const baseMonitoringConsole = baseCommon + `
splunk:
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
          kvstore:
            disabled: "true"
    - key: inputs
      value:
        directory: /opt/splunk/etc/system/local
        content:
          http:
            disabled: 1
    - key: web
      value:
        directory: /opt/splunk/etc/system/local
        content:
          settings:
            ui_inactivity_timeout: 5
            tools.sessions.timeout: 5
`

// License Manager
const baseLicense = baseCommon + `
splunk:
  conf:
    - key: server
      value:
        directory: /opt/splunk/etc/system/local
        content:
          general:
            sessionTimeout: 5m
    - key: inputs
      value:
        directory: /opt/splunk/etc/system/local
        content:
          http:
            disabled: 1
`

// Fallback
const baseGeneric = baseCommon
