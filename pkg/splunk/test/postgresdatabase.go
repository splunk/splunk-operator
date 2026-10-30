// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PostgresDatabaseBundleFixture contains a complete, fake Phase 1 publication.
type PostgresDatabaseBundleFixture struct {
	Database      *platformApi.PostgresDatabase
	Cluster       *platformApi.PostgresCluster
	Metadata      *corev1.ConfigMap
	Admin, RW, CA *corev1.Secret
}

// NewPostgresDatabaseBundleFixture generates a test-only CA without exposing its
// private key. All objects remain in memory and use the namespace ns.
func NewPostgresDatabaseBundleFixture(t *testing.T) PostgresDatabaseBundleFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return PostgresDatabaseBundleFixture{
		Database: &platformApi.PostgresDatabase{
			Name: "db", Namespace: "ns", Generation: 2,
			Spec: platformApi.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "primary"},
				Databases:  []platformApi.DatabaseDefinition{{Name: "app"}},
			},
			Status: platformApi.PostgresDatabaseStatus{
				ObservedGeneration: new(int64(2)),
				Databases: []platformApi.DatabaseInfo{{
					Name: "app", Ready: true,
					ConfigMapRef:       &corev1.LocalObjectReference{Name: "published-config"},
					AdminUserSecretRef: &corev1.SecretKeySelector{Name: "published-admin", Key: "password"},
					RWUserSecretRef:    &corev1.SecretKeySelector{Name: "external-rw", Key: "custom-password"},
				}},
			},
		},
		Cluster: &platformApi.PostgresCluster{
			Name: "primary", Namespace: "ns", Generation: 1,
			Status: platformApi.PostgresClusterStatus{Phase: new("Ready"), ObservedGeneration: new(int64(1))},
		},
		Metadata: &corev1.ConfigMap{
			Name: "published-config", Namespace: "ns",
			Data: map[string]string{
				"CLUSTER_RW_ENDPOINT": "primary-rw.ns.svc.cluster.local", "DEFAULT_CLUSTER_PORT": "5432",
				"DATABASE_NAME": "app", "ADMIN_USER_NAME": "app_admin", "RW_USER_NAME": "app_rw",
			},
		},
		Admin: &corev1.Secret{Name: "published-admin", Namespace: "ns", Data: map[string][]byte{"username": []byte("app_admin"), "password": []byte("test-admin-password")}},
		RW:    &corev1.Secret{Name: "external-rw", Namespace: "ns", Data: map[string][]byte{"username": []byte("app_rw"), "custom-password": []byte("test-rw-password")}},
		CA:    &corev1.Secret{Name: "primary-ca", Namespace: "ns", Data: map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}},
	}
}

// Objects returns the fixture objects for a fake Kubernetes client.
func (f PostgresDatabaseBundleFixture) Objects() []client.Object {
	return []client.Object{f.Database, f.Cluster, f.Metadata, f.Admin, f.RW, f.CA}
}
