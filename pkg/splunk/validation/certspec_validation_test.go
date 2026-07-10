/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// TestCertSpecFieldAccepted verifies that spec.certs[] is accepted on
// CommonSplunkSpec with valid role and no-role entries.
// The XValidation uniqueness rule (at most one entry per role) is enforced
// by the Kubernetes API server via CEL — these tests cover the Go-layer
// field shape only.
func TestCertSpecFieldAccepted(t *testing.T) {
	tests := []struct {
		name  string
		certs []enterpriseApi.CertSpec
	}{
		{
			name:  "empty certs is valid",
			certs: nil,
		},
		{
			name: "single server-role cert is valid",
			certs: []enterpriseApi.CertSpec{
				{SecretRef: corev1.LocalObjectReference{Name: "my-server-cert"}, Role: enterpriseApi.CertRoleServer},
			},
		},
		{
			name: "single input-role cert is valid",
			certs: []enterpriseApi.CertSpec{
				{SecretRef: corev1.LocalObjectReference{Name: "my-input-cert"}, Role: enterpriseApi.CertRoleInput},
			},
		},
		{
			name: "no-role cert (mount-only) is valid",
			certs: []enterpriseApi.CertSpec{
				{SecretRef: corev1.LocalObjectReference{Name: "custom-ca"}},
			},
		},
		{
			name: "mixed roles and no-role is valid",
			certs: []enterpriseApi.CertSpec{
				{SecretRef: corev1.LocalObjectReference{Name: "server-cert"}, Role: enterpriseApi.CertRoleServer},
				{SecretRef: corev1.LocalObjectReference{Name: "input-cert"}, Role: enterpriseApi.CertRoleInput},
				{SecretRef: corev1.LocalObjectReference{Name: "custom-ca"}},
			},
		},
		{
			name: "multiple no-role certs is valid",
			certs: []enterpriseApi.CertSpec{
				{SecretRef: corev1.LocalObjectReference{Name: "ca-cert-1"}},
				{SecretRef: corev1.LocalObjectReference{Name: "ca-cert-2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := enterpriseApi.CommonSplunkSpec{Certs: tt.certs}
			// Verify each CertSpec is properly stored and retrievable
			if len(spec.Certs) != len(tt.certs) {
				t.Errorf("expected %d certs, got %d", len(tt.certs), len(spec.Certs))
			}
			for i, c := range spec.Certs {
				if c.SecretRef.Name != tt.certs[i].SecretRef.Name {
					t.Errorf("cert[%d].SecretName = %q, want %q", i, c.SecretRef.Name, tt.certs[i].SecretRef.Name)
				}
				if c.Role != tt.certs[i].Role {
					t.Errorf("cert[%d].Role = %q, want %q", i, c.Role, tt.certs[i].Role)
				}
			}
		})
	}
}

// TestCertRoleConstants verifies the CertRole string values match the
// Ansible mount path convention (splunk-<role>-tls-cert).
func TestCertRoleConstants(t *testing.T) {
	if enterpriseApi.CertRoleServer != "server" {
		t.Errorf("CertRoleServer = %q, want %q", enterpriseApi.CertRoleServer, "server")
	}
	if enterpriseApi.CertRoleInput != "input" {
		t.Errorf("CertRoleInput = %q, want %q", enterpriseApi.CertRoleInput, "input")
	}
}

// TestGetCertsHelpers verifies that each v4 CR type exposes spec.certs[]
// through GetCerts(), which is used by the cert-secret watch mapper.
func TestGetCertsHelpers(t *testing.T) {
	certs := []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "test-cert"}, Role: enterpriseApi.CertRoleServer},
	}

	t.Run("Standalone", func(t *testing.T) {
		cr := &enterpriseApi.Standalone{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 || got[0].SecretRef.Name != "test-cert" {
			t.Errorf("GetCerts() = %v, want 1 cert with SecretName=test-cert", got)
		}
	})
	t.Run("IndexerCluster", func(t *testing.T) {
		cr := &enterpriseApi.IndexerCluster{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
	t.Run("SearchHeadCluster", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
	t.Run("ClusterManager", func(t *testing.T) {
		cr := &enterpriseApi.ClusterManager{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
	t.Run("LicenseManager", func(t *testing.T) {
		cr := &enterpriseApi.LicenseManager{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
	t.Run("MonitoringConsole", func(t *testing.T) {
		cr := &enterpriseApi.MonitoringConsole{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
	t.Run("IngestorCluster", func(t *testing.T) {
		cr := &enterpriseApi.IngestorCluster{}
		cr.Spec.Certs = certs
		if got := cr.GetCerts(); len(got) != 1 {
			t.Errorf("GetCerts() returned %d certs, want 1", len(got))
		}
	})
}
