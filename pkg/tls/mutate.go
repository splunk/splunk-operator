package tls

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// InjectTLSForPodTemplate wires TLS mounts/env/annotations into the PodTemplate.
//
// It expects that the reconciler already:
//   * created/updated a ConfigMap named preTasksCMName with key splunk.PreTasksCMKey holding the rendered pretasks YAML
//   * computed observedTLSChecksum (e.g., sha256 of Secret data or CSI attributes)
//   * computed preTasksHash (sha256 of rendered pretasks content)
//
// This function:
//   * mounts the pretasks CM at /mnt/pre/tls.yml and sets SPLUNK_ANSIBLE_PRE_TASKS
//   * mounts Secret or CSI at /mnt/certs
//   * mounts optional trust-bundle Secret at /mnt/trust
//   * mounts optional KV password Secret at /mnt/kvpass (and returns its file path for renderer convenience)
//   * annotates the pod template with TLS and pretask checksums (visible diff with OnDelete strategy)
func InjectTLSForPodTemplate(
	podTemplate *corev1.PodTemplateSpec,
	spec *v4.TLSConfig,
	preTasksCMName string,
	preTasksHash string,
	observedTLSChecksum string,
	kvPassMount *corev1.SecretKeySelector, // optional (used only when KVEncryptedKey.enabled)
) (kvPassFile string, err error) {
	if podTemplate == nil {
		return "", fmt.Errorf("podTemplate is nil")
	}
	if spec == nil {
		return "", fmt.Errorf("tls spec is nil")
	}
	if len(podTemplate.Spec.Containers) == 0 {
		return "", fmt.Errorf("podTemplate has no containers")
	}

	// -----------------------
	// 1) PRE-TASKS CM mount
	// -----------------------
	addVol(podTemplate, corev1.Volume{
		Name: "pre",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: preTasksCMName},
				Items: []corev1.KeyToPath{{
					Key:  PreTasksCMKey,
					Path: PreTasksFilename,
					Mode: Int32Ptr(0444),
				}},
				DefaultMode: Int32Ptr(0444),
			},
		},
	})
	addMnt(podTemplate, corev1.VolumeMount{Name: "pre", MountPath: PreTasksMountDir, ReadOnly: true})

	// ----------------------------------------------------
	// 2) TLS source (Secret or CSI) -> /mnt/certs
	// ----------------------------------------------------
	switch spec.Provider {
	case v4.TLSProviderSecret:
		if spec.SecretRef == nil || spec.SecretRef.Name == "" {
			return "", fmt.Errorf("tls.provider=Secret but secretRef is empty")
		}
		addVol(podTemplate, corev1.Volume{
			Name: "tls-src",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.SecretRef.Name,
					DefaultMode: Int32Ptr(0400),
				},
			},
		})
		addMnt(podTemplate, corev1.VolumeMount{Name: "tls-src", MountPath: TLSSrcMountDir, ReadOnly: true})

	case v4.TLSProviderCSI:
		if spec.CSI == nil {
			return "", fmt.Errorf("tls.provider=CSI but csi is nil")
		}
		attrs := map[string]string{
			"csi.storage.k8s.io/ephemeral": "true",
			"issuer-name":                   spec.CSI.IssuerRefName,
			"issuer-kind":                   spec.CSI.IssuerRefKind,
			"issuer-group":                  "cert-manager.io",
		}
		if len(spec.CSI.DNSNames) > 0 {
			attrs["dns-names"] = joinCSV(spec.CSI.DNSNames)
		}
		if spec.CSI.Duration != "" {
			attrs["duration"] = spec.CSI.Duration
		}
		if spec.CSI.RenewBefore != "" {
			attrs["renew-before"] = spec.CSI.RenewBefore
		}
		if spec.CSI.KeyAlgorithm != "" {
			attrs["key-algorithm"] = spec.CSI.KeyAlgorithm
		}
		if spec.CSI.KeySize > 0 {
			attrs["key-size"] = fmt.Sprintf("%d", spec.CSI.KeySize)
		}

		addVol(podTemplate, corev1.Volume{
			Name: "tls-src",
			VolumeSource: corev1.VolumeSource{
				CSI: &corev1.CSIVolumeSource{
					Driver:           "csi.cert-manager.io",
					ReadOnly:         BoolPtr(true),
					VolumeAttributes: attrs,
				},
			},
		})
		addMnt(podTemplate, corev1.VolumeMount{Name: "tls-src", MountPath: TLSSrcMountDir, ReadOnly: true})

	default:
		return "", fmt.Errorf("unsupported tls.provider: %q", string(spec.Provider))
	}

	// ---------------------------------------------
	// 3) Optional trust-bundle Secret -> /mnt/trust
	// ---------------------------------------------
	if spec.TrustBundle != nil && spec.TrustBundle.SecretName != "" {
		addVol(podTemplate, corev1.Volume{
			Name: "trust-src",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.TrustBundle.SecretName,
					DefaultMode: Int32Ptr(0444),
				},
			},
		})
		addMnt(podTemplate, corev1.VolumeMount{Name: "trust-src", MountPath: TrustSrcMountDir, ReadOnly: true})
	}

	// --------------------------------------------------------
	// 4) Optional KV password Secret -> /mnt/kvpass/<key>
	// --------------------------------------------------------
	if spec.KVEncryptedKey != nil && spec.KVEncryptedKey.Enabled && kvPassMount != nil {
		if kvPassMount.Name != "" {
			addVol(podTemplate, corev1.Volume{
				Name: "kvpass",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  kvPassMount.Name,
						DefaultMode: Int32Ptr(0400),
					},
				},
			})
			addMnt(podTemplate, corev1.VolumeMount{Name: "kvpass", MountPath: KVPassSrcMountDir, ReadOnly: true})
			if kvPassMount.Key != "" {
				kvPassFile = KVPassSrcMountDir + "/" + kvPassMount.Key
			}
		}
	}

	// --------------------------------------------------------
	// 5) Env on Splunk container
	// --------------------------------------------------------
	c := &podTemplate.Spec.Containers[0]
	c.Env = upsertEnv(c.Env, corev1.EnvVar{Name: EnvPreTasks, Value: PreTasksFileURI})
	// After InjectTLSForPodTemplate(...)
	if len(podTemplate.Spec.Containers) == 0 {
		return kvPassFile, fmt.Errorf("no containers in pod template")
	}
	spl := &podTemplate.Spec.Containers[0]
	add := func(n, v string) { 
		found := false
		for i := range spl.Env { if spl.Env[i].Name == n { spl.Env[i].Value = v; found = true; break } }
		if !found { spl.Env = append(spl.Env, corev1.EnvVar{Name: n, Value: v}) }
	}

	// Web SSL (keeps Ansible roles consistent with our file layout)
	add("SPLUNK_HTTP_ENABLESSL", "1")
	add("SPLUNK_HTTP_ENABLESSL_CERT", fmt.Sprintf("%s/%s", CanonicalTLSDir, TLSCrtName))
	add("SPLUNK_HTTP_ENABLESSL_PRIVKEY", fmt.Sprintf("%s/%s", CanonicalTLSDir, TLSKeyName))
	add("SPLUNK_HTTP_ENABLESSL_PRIVKEY_PASSWORD", "")

	// splunkd SSL (we point to our combined server.pem and CA)
	add("SPLUNKD_SSL_ENABLE", "true")
	add("SPLUNKD_SSL_CERT", fmt.Sprintf("%s/%s", CanonicalTLSDir, ServerPEMName))
	add("SPLUNKD_SSL_CA", fmt.Sprintf("%s/%s", CanonicalTLSDir, CACrtName))

	// --------------------------------------------------------
	// 6) Annotations for visibility (even with OnDelete)
	// --------------------------------------------------------
	if podTemplate.Annotations == nil {
		podTemplate.Annotations = map[string]string{}
	}
	podTemplate.Annotations[AnnTLSChecksum] = safeTrunc(observedTLSChecksum)
	podTemplate.Annotations[AnnPreTasksChecksum] = safeTrunc(preTasksHash)

	return kvPassFile, nil
}

// --- helpers ---

func addVol(pt *corev1.PodTemplateSpec, v corev1.Volume) {
	pt.Spec.Volumes = append(pt.Spec.Volumes, v)
}

func addMnt(pt *corev1.PodTemplateSpec, m corev1.VolumeMount) {
	c := &pt.Spec.Containers[0]
	c.VolumeMounts = append(c.VolumeMounts, m)
}

func upsertEnv(env []corev1.EnvVar, nv corev1.EnvVar) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == nv.Name {
			env[i] = nv
			return env
		}
	}
	return append(env, nv)
}

func joinCSV(ss []string) string {
	cp := append([]string(nil), ss...)
	sort.Strings(cp)
	return strings.Join(cp, ",")
}

func safeTrunc(s string) string {
	if len(s) <= 64 {
		return s
	}
	return s[:64]
}
