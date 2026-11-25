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
	"github.com/splunk/splunk-operator/pkg/splunk/common"
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

// PostgresProjectedFile names the producer resource and key behind one mounted file.
type PostgresProjectedFile struct {
	Source, Key string
}

// PostgresDatabaseFiles maps each file path in a pod's PostgresDatabase
// projection to its source, independent of the projection's source order.
func PostgresDatabaseFiles(t testing.TB, pod *corev1.PodTemplateSpec) map[string]PostgresProjectedFile {
	t.Helper()
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != common.PostgresDatabaseVolumeName || volume.Projected == nil {
			continue
		}
		files := make(map[string]PostgresProjectedFile)
		add := func(source string, items []corev1.KeyToPath) {
			for _, item := range items {
				if _, duplicate := files[item.Path]; duplicate {
					t.Fatalf("PostgresDatabase projection maps %q twice", item.Path)
				}
				files[item.Path] = PostgresProjectedFile{Source: source, Key: item.Key}
			}
		}
		for _, source := range volume.Projected.Sources {
			switch {
			case source.ConfigMap != nil:
				add(source.ConfigMap.Name, source.ConfigMap.Items)
			case source.Secret != nil:
				add(source.Secret.Name, source.Secret.Items)
			}
		}
		return files
	}
	t.Fatal("missing PostgresDatabase projected volume")
	return nil
}
