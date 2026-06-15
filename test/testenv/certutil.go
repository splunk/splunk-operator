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

	"github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/test/testdata/certs"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateServerCertSecret creates a K8s TLS secret containing the pre-baked server
// certificate (CN=splunk-test.cluster.local, signed by the test CA, expires 2036).
// The secret has tls.crt, tls.key, and ca.crt — the format SOK expects.
// No cert-manager required.
func CreateServerCertSecret(ctx context.Context, c client.Client, ns, secretName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certs.ServerCRT,
			"tls.key": certs.ServerKey,
			"ca.crt":  certs.CACrt,
		},
	}
	return c.Create(ctx, secret)
}

// CreateCustomCertSecret creates a K8s TLS secret containing the pre-baked custom
// (no-role) certificate. Used to test as-is mount path behaviour.
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

// RotateCertSecret simulates cert rotation by updating tls.crt with new content
// (swapping server cert ↔ custom cert so the data actually changes), then
// verifying WaitForCertSecret still resolves.
// This is sufficient to trigger the certRev hash change without needing cert-manager.
func RotateCertSecret(ctx context.Context, deployment *Deployment, c client.Client, ns, secretName string) {
	secret := &corev1.Secret{}
	gomega.Expect(c.Get(ctx,
		ktypes.NamespacedName{Namespace: ns, Name: secretName}, secret)).To(gomega.Succeed())

	// Swap in different cert bytes so the SHA-256 hash changes.
	// We use the custom cert material as the "rotated" content.
	updated := secret.DeepCopy()
	updated.Data["tls.crt"] = certs.CustomCRT
	updated.Data["tls.key"] = certs.CustomKey
	gomega.Expect(c.Update(ctx, updated)).To(gomega.Succeed())
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
	}, DefaultTimeout, PollInterval).Should(gomega.BeTrue(),
		"pod %q did not reach Running state", podName)
}

// WaitForCertSecret polls until the named secret exists with tls.crt and tls.key populated.
func WaitForCertSecret(ctx context.Context, deployment *Deployment, secretName string) {
	gomega.Eventually(func() bool {
		secret := &corev1.Secret{}
		if err := deployment.GetInstance(ctx, secretName, secret); err != nil {
			return false
		}
		_, hasCrt := secret.Data["tls.crt"]
		_, hasKey := secret.Data["tls.key"]
		return hasCrt && hasKey
	}, DefaultTimeout, PollInterval).Should(gomega.BeTrue(),
		"cert secret %q not ready", secretName)
}

// VerifyCertSecretMounted asserts that tls.crt is present at mountPath inside podName.
func VerifyCertSecretMounted(ctx context.Context, deployment *Deployment, podName, mountPath string) {
	stdin := fmt.Sprintf("ls %s/tls.crt && echo OK", mountPath)
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName,
		[]string{"/bin/sh"}, stdin, false)
	gomega.Expect(err).To(gomega.Succeed(),
		"exec failed on pod %s path %s: stderr=%s", podName, mountPath, stderr)
	gomega.Expect(stdout).To(gomega.ContainSubstring("OK"),
		"tls.crt not found at %s on pod %s", mountPath, podName)
}

// VerifyCertRevAnnotation asserts that the StatefulSet named stsName has a
// non-empty certRev/<secretName> annotation on its pod template.
func VerifyCertRevAnnotation(ctx context.Context, c client.Client, ns, stsName, secretName string) {
	annotKey := fmt.Sprintf("enterprise.splunk.com/cert-rev-%s", secretName)
	gomega.Eventually(func() string {
		return GetCertRevAnnotation(ctx, c, ns, stsName, secretName)
	}, DefaultTimeout, PollInterval).ShouldNot(gomega.BeEmpty(),
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
	}, DefaultTimeout, PollInterval).ShouldNot(gomega.Equal(initialHash),
		"certRev/%s on StatefulSet %s did not change after cert rotation", secretName, stsName)
}
