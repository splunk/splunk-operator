package tls

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// StatusUpdater is implemented by each CR controller (Standalone, SHC, etc.).
// Keep this thin and easy to implement.
type StatusUpdater interface {
	GetClient() client.Client
	NamespacedName() types.NamespacedName

	SpecTLS() *v4.TLSConfig
	ExistingTLSStatus() *v4.TLSStatus
	UpdateTLSStatus(ctx context.Context, st *v4.TLSStatus) error

	// For PRE_TASKS hashing and surfacing what is really applied
	PreTasksConfigMapName() string             // e.g. <crName>-tls-pre
	PodTemplateAnnotations() map[string]string // from the current PodTemplate
}

// ObserveAndUpdate computes current TLS state and writes Status if changed.
// Does not block reconcile; callers may ignore errors.
func ObserveAndUpdate(ctx context.Context, u StatusUpdater) error {
	cfg := u.SpecTLS()
	if cfg == nil {
		return nil
	}
	now := metav1.NewTime(time.Now().UTC())

	st := &v4.TLSStatus{
		Provider:     cfg.Provider,
		CanonicalDir: firstNonEmpty(cfg.CanonicalDir, CanonicalTLSDir),
		Conditions:   []v4.TLSCondition{},
	}

	switch cfg.Provider {
	case v4.TLSProviderSecret:
		obs, bundleHash, conds, err := observeFromSecret(ctx, u.GetClient(), u.NamespacedName().Namespace, cfg)
		if err != nil {
			conds = append(conds, condition(v4.TLSReady, corev1.ConditionFalse, "SecretReadError", err.Error()))
		}
		st.Observed = obs
		st.TrustBundleHash = bundleHash
		st.Conditions = append(st.Conditions, conds...)

	case v4.TLSProviderCSI:
		// Best-effort fingerprint from CSI attributes
		obs := v4.TLSObserved{
			Source: fmt.Sprintf("CSI:%s/%s", safe(cfg.CSI.IssuerRefKind), safe(cfg.CSI.IssuerRefName)),
			Hash:   hashCSIAttributes(cfg),
		}
		st.Observed = obs

		// Optional trust bundle remains observable
		if cfg.TrustBundle != nil && cfg.TrustBundle.SecretName != "" {
			if h, err := hashSingleKeySecret(ctx, u.GetClient(), u.NamespacedName().Namespace, cfg.TrustBundle.SecretName, firstNonEmpty(cfg.TrustBundle.Key, "ca-bundle.crt")); err == nil && h != "" {
				st.TrustBundleHash = h
				st.Conditions = append(st.Conditions, condition(v4.TLSTrustBundleReady, corev1.ConditionTrue, "Observed", "Trust bundle Secret observed"))
			}
		}
		st.Conditions = append(st.Conditions, condition(v4.TLSTrackingLimited, corev1.ConditionTrue, "CSIContentNotObservable", "Using best-effort hash from CSI attributes"))

	default:
		// Unknown provider; leave status minimal but not failing the controller
		st.Conditions = append(st.Conditions, condition(v4.TLSReady, corev1.ConditionFalse, "UnsupportedProvider", string(cfg.Provider)))
	}

	// Hash the PRE_TASKS content we apply - based on the live ConfigMap
	if cmHash, err := hashPreTasksConfigMap(ctx, u); err == nil && cmHash != "" {
		st.PreTasksHash = cmHash
	}

	// Surface current PodTemplate TLS checksum (annotation is set during mutate)
	if ann := u.PodTemplateAnnotations(); ann != nil {
		if v, ok := ann[AnnTLSChecksum]; ok {
			st.PodTemplateChecksum = v
		}
	}

	// Ready when we have any observed fingerprint
	if st.Observed.Hash != "" {
		st.Conditions = upsert(st.Conditions, condition(v4.TLSReady, corev1.ConditionTrue, "Observed", "TLS material observed"))
	}
	st.LastObserved = now

	// Rotation signal - compare with previous
	prev := u.ExistingTLSStatus()
	if rotationNeeded(prev, st) {
		st.Conditions = upsert(st.Conditions, condition(v4.TLSRotatePending, corev1.ConditionTrue, "MaterialChanged", "TLS material changed, restart required for OnDelete"))
	} else {
		st.Conditions = upsert(st.Conditions, condition(v4.TLSRotatePending, corev1.ConditionFalse, "NoChange", "No TLS material change detected"))
	}

	// Persist if any change
	if !tlsStatusEqual(prev, st) {
		return u.UpdateTLSStatus(ctx, st)
	}
	return nil
}

func observeFromSecret(ctx context.Context, c client.Client, ns string, cfg *v4.TLSConfig) (v4.TLSObserved, string, []v4.TLSCondition, error) {
	var conds []v4.TLSCondition
	obs := v4.TLSObserved{}

	if cfg.SecretRef == nil || cfg.SecretRef.Name == "" {
		return obs, "", conds, fmt.Errorf("secretRef.name required for provider=Secret")
	}

	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: cfg.SecretRef.Name}, sec); err != nil {
		return obs, "", conds, err
	}
	obs.Source = "Secret/" + sec.Name
	obs.SecretResourceVersion = sec.ResourceVersion

	// Hash over presence + bytes of tls.crt, tls.key, ca.crt
	keys := []string{"tls.crt", "tls.key", "ca.crt"}
	h := sha256.New()
	for _, k := range keys {
		if b, ok := sec.Data[k]; ok {
			h.Write([]byte(k + ":"))
			h.Write(b)
		} else {
			h.Write([]byte(k + ":<nil>"))
		}
	}
	obs.Hash = hex.EncodeToString(h.Sum(nil))

	// Parse leaf certificate best-effort
	if crtBytes, ok := sec.Data["tls.crt"]; ok && len(crtBytes) > 0 {
		if leaf := parseFirstCert(crtBytes); leaf != nil {
			lh := sha256.Sum256(leaf.Raw)
			obs.LeafSHA256 = hex.EncodeToString(lh[:])
			nb := metav1.NewTime(leaf.NotBefore.UTC())
			na := metav1.NewTime(leaf.NotAfter.UTC())
			obs.NotBefore, obs.NotAfter = &nb, &na
			obs.SerialNumber = leaf.SerialNumber.String()
			conds = append(conds, condition(v4.TLSReady, corev1.ConditionTrue, "Observed", "Secret with tls.crt parsed"))
		}
	}

	// Trust bundle hash if configured
	bundleHash := ""
	if cfg.TrustBundle != nil && cfg.TrustBundle.SecretName != "" {
		if h, err := hashSingleKeySecret(ctx, c, ns, cfg.TrustBundle.SecretName, firstNonEmpty(cfg.TrustBundle.Key, "ca-bundle.crt")); err == nil && h != "" {
			bundleHash = h
			conds = append(conds, condition(v4.TLSTrustBundleReady, corev1.ConditionTrue, "Observed", "Trust bundle Secret observed"))
		}
	}

	return obs, bundleHash, conds, nil
}

// hashPreTasksConfigMap returns the sha256 of the live pretasks ConfigMap data (tls.yml).
func hashPreTasksConfigMap(ctx context.Context, u StatusUpdater) (string, error) {
	name := u.PreTasksConfigMapName()
	if name == "" {
		return "", nil
	}
	cm := &corev1.ConfigMap{}
	if err := u.GetClient().Get(ctx, types.NamespacedName{Namespace: u.NamespacedName().Namespace, Name: name}, cm); err != nil {
		return "", nil // not fatal; just means we can't surface the hash yet
	}
	if s, ok := cm.Data[PreTasksCMKey]; ok && s != "" {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:]), nil
	}
	return "", nil
}

func hashSingleKeySecret(ctx context.Context, c client.Client, ns, name, key string) (string, error) {
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		return "", err
	}
	b, ok := sec.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q missing", key)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func hashCSIAttributes(cfg *v4.TLSConfig) string {
	if cfg == nil || cfg.CSI == nil {
		return ""
	}
	parts := []string{
		safe(cfg.CSI.IssuerRefKind),
		safe(cfg.CSI.IssuerRefName),
		strings.Join(append([]string{}, cfg.CSI.DNSNames...), ","),
		safe(cfg.CSI.Duration),
		safe(cfg.CSI.RenewBefore),
		safe(cfg.CSI.KeyAlgorithm),
		fmt.Sprintf("%d", cfg.CSI.KeySize),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func rotationNeeded(prev, cur *v4.TLSStatus) bool {
	if prev == nil {
		return false
	}
	if prev.Observed.Hash != cur.Observed.Hash {
		return true
	}
	if prev.TrustBundleHash != cur.TrustBundleHash {
		return true
	}
	if prev.PreTasksHash != cur.PreTasksHash {
		return true
	}
	// PodTemplateChecksum is informational; do not use it to trigger rotation.
	return false
}

func tlsStatusEqual(a, b *v4.TLSStatus) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Provider == b.Provider &&
		a.CanonicalDir == b.CanonicalDir &&
		a.Observed.Hash == b.Observed.Hash &&
		a.Observed.SecretResourceVersion == b.Observed.SecretResourceVersion &&
		a.TrustBundleHash == b.TrustBundleHash &&
		a.PreTasksHash == b.PreTasksHash &&
		a.PodTemplateChecksum == b.PodTemplateChecksum
}

func condition(t v4.TLSConditionType, s corev1.ConditionStatus, reason, msg string) v4.TLSCondition {
	return v4.TLSCondition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
}

func upsert(list []v4.TLSCondition, c v4.TLSCondition) []v4.TLSCondition {
	// replace if same Type exists
	for i := range list {
		if list[i].Type == c.Type {
			list[i] = c
			sort.Slice(list, func(i, j int) bool { return list[i].Type < list[j].Type })
			return list
		}
	}
	// not found: append
	list = append(list, c)
	sort.Slice(list, func(i, j int) bool { return list[i].Type < list[j].Type })
	return list
}


// parseFirstCert handles both PEM and raw DER; returns the first cert if found.
func parseFirstCert(b []byte) *x509.Certificate {
	// Try PEM first (possibly a chain)
	var rest = b
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			if c, err := x509.ParseCertificate(block.Bytes); err == nil {
				return c
			}
		}
	}
	// Fallback: try raw DER bundle
	if certs, err := x509.ParseCertificates(b); err == nil && len(certs) > 0 {
		return certs[0]
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func safe(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
