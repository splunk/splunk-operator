// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"context"
	"fmt"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/splunk/splunk-operator/test/testdata/certs"
)

// CreateCustomCertSecret creates a K8s TLS secret containing a pre-baked
// certificate (tls.crt+tls.key only, no ca.crt). Used to test the as-is mount
// path for a cert that already exists in the cluster at spec time (no
// cert-manager auto-generation involved).
func CreateCustomCertSecret(ctx context.Context, c client.Client, ns, secretName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certs.CustomCRT,
			"tls.key": certs.CustomKey,
		},
	}
	return c.Create(ctx, secret)
}

// CreateSelfSignedIssuer creates a cert-manager self-signed Issuer in ns.
// Used by Phase 2 auto-generation tests as a minimal, no-external-dependency
// issuer that cert-manager can use to mint certificates immediately.
func CreateSelfSignedIssuer(ctx context.Context, c client.Client, ns, name string) error {
	issuer := &cmapi.Issuer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: cmapi.IssuerSpec{
			IssuerConfig: cmapi.IssuerConfig{
				SelfSigned: &cmapi.SelfSignedIssuer{},
			},
		},
	}
	return c.Create(ctx, issuer)
}

// WaitForPodRunning polls until the named pod has at least one container in Running state.
// Used by cert tests to verify mounts as soon as any pod starts — no need to wait for
// full cluster Ready, since cert volumes are mounted at pod creation time.
func WaitForPodRunning(ctx context.Context, deployment *Deployment, podName string) {
	gomega.Eventually(func() bool {
		pod := &corev1.Pod{}
		if err := deployment.GetInstance(ctx, podName, pod); err != nil {
			return false
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Running != nil {
				return true
			}
		}
		return false
	}, CertTimeout, PollInterval).Should(gomega.BeTrue(),
		"pod %q did not reach Running state", podName)
}

// WaitForCertSecret polls until the named secret exists with tls.crt and tls.key populated.
func WaitForCertSecret(ctx context.Context, deployment *Deployment, secretName string) {
	WaitForCertSecretKeys(ctx, deployment, secretName, "tls.crt", "tls.key")
}

// WaitForCertSecretKeys polls until the named secret exists with all of keys populated.
// Use this over WaitForCertSecret when the expected shape includes ca.crt, e.g. an
// auto-generated cert from a self-signed issuer (which populates ca.crt too).
func WaitForCertSecretKeys(ctx context.Context, deployment *Deployment, secretName string, keys ...string) {
	gomega.Eventually(func() bool {
		secret := &corev1.Secret{}
		if err := deployment.GetInstance(ctx, secretName, secret); err != nil {
			return false
		}
		for _, key := range keys {
			if _, ok := secret.Data[key]; !ok {
				return false
			}
		}
		return true
	}, CertTimeout, PollInterval).Should(gomega.BeTrue(),
		"cert secret %q not ready with keys %v", secretName, keys)
}

// VerifyCertSecretMounted asserts that tls.crt is present at mountPath inside podName.
// Retries via Eventually: podName may still resolve to the pod's pre-update revision
// for a while after a cert is added to spec.Certs, since the StatefulSet controller
// recreates the pod (to pick up the new volume mount) asynchronously from the API
// update WaitForPodRunning observed.
func VerifyCertSecretMounted(ctx context.Context, deployment *Deployment, podName, mountPath string) {
	VerifyCertFileMounted(ctx, deployment, podName, mountPath, "tls.crt")
}

// VerifyCertFileMounted asserts that fileName is present at mountPath inside podName.
// Same retry rationale as VerifyCertSecretMounted; use this variant to check a
// specific file (e.g. "ca.crt") rather than the default "tls.crt".
func VerifyCertFileMounted(ctx context.Context, deployment *Deployment, podName, mountPath, fileName string) {
	stdin := fmt.Sprintf("ls %s/%s && echo OK", mountPath, fileName)
	gomega.Eventually(func() string {
		stdout, _, err := deployment.PodExecCommand(ctx, podName,
			[]string{"/bin/sh"}, stdin, false)
		if err != nil {
			return ""
		}
		return stdout
	}, CertTimeout, PollInterval).Should(gomega.ContainSubstring("OK"),
		"%s not found at %s on pod %s", fileName, mountPath, podName)
}

// VerifyCertRevAnnotation asserts that the StatefulSet named stsName has a
// non-empty certRev/<secretName> annotation on its pod template.
func VerifyCertRevAnnotation(ctx context.Context, c client.Client, ns, stsName, secretName string) {
	annotKey := fmt.Sprintf("enterprise.splunk.com/cert-rev-%s", secretName)
	gomega.Eventually(func() string {
		return GetCertRevAnnotation(ctx, c, ns, stsName, secretName)
	}, CertTimeout, PollInterval).ShouldNot(gomega.BeEmpty(),
		"certRev/%s annotation not set on StatefulSet %s", secretName, stsName)
	_ = annotKey
}

// GetCertRevAnnotation returns the current certRev/<secretName> annotation value
// from the StatefulSet pod template, or empty string if not found.
func GetCertRevAnnotation(ctx context.Context, c client.Client, ns, stsName, secretName string) string {
	annotKey := fmt.Sprintf("enterprise.splunk.com/cert-rev-%s", secretName)
	sts := &unstructured.Unstructured{}
	sts.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"})
	if err := c.Get(ctx, ktypes.NamespacedName{Namespace: ns, Name: stsName}, sts); err != nil {
		return ""
	}
	annots, _, _ := unstructured.NestedStringMap(sts.Object, "spec", "template", "metadata", "annotations")
	return annots[annotKey]
}

// VerifyCertRotation asserts that after rotation the certRev annotation changes
// from initialHash to a different value.
func VerifyCertRotation(ctx context.Context, c client.Client, ns, stsName, secretName, initialHash string) {
	gomega.Eventually(func() string {
		return GetCertRevAnnotation(ctx, c, ns, stsName, secretName)
	}, CertTimeout, PollInterval).ShouldNot(gomega.Equal(initialHash),
		"certRev/%s on StatefulSet %s did not change after cert rotation", secretName, stsName)
}

// RotateCertSecret simulates cert rotation by swapping a secret's tls.crt/tls.key
// for different pre-baked cert material, so the certRev SHA-256 hash changes
// without depending on cert-manager to reissue (which the operator never
// requests once a Certificate CR exists).
func RotateCertSecret(ctx context.Context, c client.Client, ns, secretName string) {
	secret := &corev1.Secret{}
	gomega.Expect(c.Get(ctx, ktypes.NamespacedName{Namespace: ns, Name: secretName}, secret)).To(gomega.Succeed())

	updated := secret.DeepCopy()
	updated.Data["tls.crt"] = certs.ServerCRT
	updated.Data["tls.key"] = certs.ServerKey
	gomega.Expect(c.Update(ctx, updated)).To(gomega.Succeed())
}

// VerifyCertificateDeleted polls until the named cert-manager Certificate no
// longer exists in ns. Used to confirm that an auto-generated Certificate CR
// was garbage-collected (via its ownerReference to the CR) after the owning
// CR was deleted.
func VerifyCertificateDeleted(ctx context.Context, c client.Client, ns, certName string) {
	gomega.Eventually(func() bool {
		cert := &cmapi.Certificate{}
		err := c.Get(ctx, ktypes.NamespacedName{Namespace: ns, Name: certName}, cert)
		return errors.IsNotFound(err)
	}, CertTimeout, PollInterval).Should(gomega.BeTrue(),
		"certificate %q was not deleted", certName)
}

// VerifySecretStillExists asserts that the named Secret still exists in ns
// and continues to exist for ConsistentDuration. Used to confirm that a
// customer-provided (never auto-generated, never CR-owned) Secret is not
// swept up by CR-deletion garbage collection.
func VerifySecretStillExists(ctx context.Context, c client.Client, ns, secretName string) {
	gomega.Consistently(func() error {
		secret := &corev1.Secret{}
		return c.Get(ctx, ktypes.NamespacedName{Namespace: ns, Name: secretName}, secret)
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Succeed(),
		"secret %q was unexpectedly deleted", secretName)
}

// VerifyServerCertTLS asserts that podName's 8089 management port presents a
// valid TLS certificate rooted at the given CA bundle path (as configured by
// Ansible's configure_server_cert.yml into server.conf's [sslConfig]). Uses
// curl with --cacert (real chain verification) and --resolve to map the
// cert's FQDN onto the pod's loopback interface — curl -k would pass even
// with a broken/wrong cert, so it must not be used here.
func VerifyServerCertTLS(ctx context.Context, deployment *Deployment, podName, fqdn string) {
	stdin := fmt.Sprintf(`curl -sv --connect-timeout 5 --max-time 10 "https://%s:8089/services/server/info" `+
		`--resolve "%s:8089:127.0.0.1" `+
		`--cacert /opt/splunk/etc/auth/splunk-server-ca.pem `+
		`-u "admin:$(cat /mnt/splunk-secrets/password)" `+
		`-o /dev/null 2>&1`, fqdn, fqdn)
	gomega.Eventually(func() string {
		stdout, _, err := deployment.PodExecCommand(ctx, podName, []string{"/bin/sh"}, stdin, false)
		if err != nil {
			return ""
		}
		return stdout
	}, CertTimeout, PollInterval).Should(gomega.And(
		gomega.ContainSubstring("SSL connection using"),
		gomega.ContainSubstring("HTTP/1.1 200"),
	), "TLS verification against %s:8089 failed on pod %s", fqdn, podName)
	logf.Log.Info("VerifyServerCertTLS complete", "podName", podName)
}
