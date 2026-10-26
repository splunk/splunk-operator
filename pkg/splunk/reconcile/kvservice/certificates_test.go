// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKVServiceCertificateReconcileUsesFinalDuplicateMount(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, true)
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	secret := &corev1.Secret{
		Name: "serving-cert", Namespace: "test",
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{certs.CertTLSCRTKey: []byte("certificate"), certs.CertTLSKeyKey: []byte("private-key")},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	cr := &enterpriseApi.KVService{
		Name: "kv", Namespace: "test", UID: types.UID("kvservice-id"),
		Spec: enterpriseApi.KVServiceSpec{},
	}
	cr.Spec.Certs = []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "serving-cert"}, Role: enterpriseApi.CertRoleServer},
		{SecretRef: corev1.LocalObjectReference{Name: "serving-cert"}, Role: enterpriseApi.CertRoleInput},
	}

	mounts, env, err := reconcileKVServiceCertificates(t.Context(), client, cr)
	require.NoError(t, err)
	require.NotNil(t, mounts)
	require.Len(t, mounts.VolumeMounts, 1)
	wantMountPath := "/mnt/tls/splunk-input-tls-cert"
	assert.Equal(t, wantMountPath, mounts.VolumeMounts[0].MountPath)
	wantCertificate := wantMountPath + "/tls.crt"
	wantPrivateKey := wantMountPath + "/tls.key"
	assert.Equal(t, wantCertificate, envValue(env, "TLS_CERTIFICATE"))
	assert.Equal(t, wantPrivateKey, envValue(env, "TLS_PRIVATE_KEY"))
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, variable := range env {
		if variable.Name == name {
			return variable.Value
		}
	}
	return ""
}

func TestKVServiceServingCertificateRequiresKeyPair(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string][]byte
		wantKey string
	}{
		{name: "CA only", data: map[string][]byte{certs.CertCAKey: []byte("ca")}, wantKey: certs.CertTLSCRTKey},
		{name: "missing private key", data: map[string][]byte{certs.CertTLSCRTKey: []byte("cert")}, wantKey: certs.CertTLSKeyKey},
		{name: "empty certificate", data: map[string][]byte{certs.CertTLSCRTKey: nil, certs.CertTLSKeyKey: []byte("key")}, wantKey: certs.CertTLSCRTKey},
		{name: "empty private key", data: map[string][]byte{certs.CertTLSCRTKey: []byte("cert"), certs.CertTLSKeyKey: nil}, wantKey: certs.CertTLSKeyKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, true)
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			secret := &corev1.Secret{Name: "serving-cert", Namespace: "test", Data: tt.data}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
			cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kv", Namespace: "test"}}
			cr.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: secret.Name}, Role: enterpriseApi.CertRoleServer}}
			mounts, env, err := reconcileKVServiceCertificates(t.Context(), c, cr)
			require.ErrorContains(t, err, tt.wantKey)
			assert.Nil(t, mounts)
			assert.Nil(t, env)
		})
	}
}

func TestKVServiceServingCertificateSelectionAndRotation(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, true)
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	trust := &corev1.Secret{Name: "trust-ca", Namespace: "test", Data: map[string][]byte{certs.CertCAKey: []byte("ca")}}
	serving := &corev1.Secret{Name: "serving-cert", Namespace: "test", Data: map[string][]byte{certs.CertTLSCRTKey: []byte("cert-v1"), certs.CertTLSKeyKey: []byte("key-v1")}}
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kv", Namespace: "test"}}
	cr.Spec.Certs = []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: trust.Name}},
		{SecretRef: corev1.LocalObjectReference{Name: serving.Name}, Role: enterpriseApi.CertRoleServer},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(trust, serving, cr).Build()
	before, env, err := reconcileKVServiceCertificates(t.Context(), c, cr)
	require.NoError(t, err)
	require.NotNil(t, before)
	require.Len(t, before.VolumeMounts, 2)
	assert.Equal(t, "/mnt/tls/splunk-server-tls-cert/tls.crt", envValue(env, "TLS_CERTIFICATE"))
	assert.Equal(t, "/mnt/tls/splunk-server-tls-cert/tls.key", envValue(env, "TLS_PRIVATE_KEY"))
	serving.Data[certs.CertTLSCRTKey] = []byte("cert-v2")
	serving.Data[certs.CertTLSKeyKey] = []byte("key-v2")
	require.NoError(t, c.Update(t.Context(), serving))
	requests := certs.CertSecretMapper(c, &enterpriseApi.KVServiceList{})(t.Context(), serving)
	require.Len(t, requests, 1)
	assert.Equal(t, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, requests[0].NamespacedName)
	after, updatedEnv, err := reconcileKVServiceCertificates(t.Context(), c, cr)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.NotEqual(t, before.Annotations, after.Annotations, "renewal must change rollout annotations")
	assert.Equal(t, before.VolumeMounts, after.VolumeMounts)
	assert.Equal(t, env, updatedEnv)
}
