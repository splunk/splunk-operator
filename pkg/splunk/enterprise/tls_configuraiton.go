// pkg/splunk/enterprise/tls_configuration.go
package enterprise

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v4 "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	tls "github.com/splunk/splunk-operator/pkg/tls"
)

func mutateTLS(ctx context.Context, c splcommon.ControllerClient, ss *appsv1.StatefulSet, cr *v4.Standalone) error {
	tlsSpec := cr.Spec.TLS
	if tlsSpec == nil {
		return nil
	}

	preCMName := fmt.Sprintf("%s-tls-pretasks", cr.Name)

	// Optional: set [general] serverName; empty is fine
	serverName := ""

	// KV password secret (optional)
	var kvPassSel *corev1.SecretKeySelector
	var kvPassFile string
	if tlsSpec.KVEncryptedKey != nil && tlsSpec.KVEncryptedKey.Enabled && tlsSpec.KVEncryptedKey.PasswordSecretRef != nil {
		kvPassSel = tlsSpec.KVEncryptedKey.PasswordSecretRef
		if kvPassSel.Key != "" {
			kvPassFile = tls.KVPassSrcMountDir + "/" + kvPassSel.Key
		}
	}

	// Best-effort checksum for CSI (Secret case is observed in status path)
	observedTLSChecksum := ""
	if tlsSpec.Provider == v4.TLSProviderCSI && tlsSpec.CSI != nil {
		observedTLSChecksum = hashCSIAttrs(tlsSpec)
	}

	// 1) Render pretasks
	preYAML, preHash, err := tls.Render(tlsSpec, serverName, kvPassFile)
	if err != nil {
		return fmt.Errorf("render pretasks: %w", err)
	}

	// 2) Create/Update the pretasks ConfigMap (vanilla metav1.ObjectMeta)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      preCMName,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"app.kubernetes.io/name":       "splunk",
				"enterprise.splunk.com/owner":  cr.Name,
			},
		},
		Data: map[string]string{
			tls.PreTasksCMKey: preYAML, // usually "tls.yml"
		},
	}
	if _, err := splkcontroller.ApplyConfigMap(ctx, c, cm); err != nil {
		return fmt.Errorf("apply pretasks configmap: %w", err)
	}

	// 3) Inject volumes/mounts/env/annotations into the pod template
	_, err = tls.InjectTLSForPodTemplate(
		&ss.Spec.Template,
		tlsSpec,
		preCMName,           // ConfigMap to mount at /mnt/pre/tls.yml
		preHash,             // annotation for pretask content hash
		observedTLSChecksum, // annotation for TLS checksum (CSI best-effort)
		kvPassSel,           // optional KV pass Secret
	)
	if err != nil {
		return fmt.Errorf("inject tls into pod template: %w", err)
	}

	return nil
}

func hashCSIAttrs(cfg *v4.TLSConfig) string {
	if cfg == nil || cfg.CSI == nil {
		return ""
	}
	parts := []string{
		safe(cfg.CSI.IssuerRefKind),
		safe(cfg.CSI.IssuerRefName),
		strings.Join(sortedCopy(cfg.CSI.DNSNames), ","),
		safe(cfg.CSI.Duration),
		safe(cfg.CSI.RenewBefore),
		safe(cfg.CSI.KeyAlgorithm),
		fmt.Sprintf("%d", cfg.CSI.KeySize),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func sortedCopy(in []string) []string {
	cp := append([]string(nil), in...)
	sort.Strings(cp)
	return cp
}

func safe(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
