// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"errors"
	"slices"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEvaluateTenantOutcomes(t *testing.T) {
	unavailable := errors.New("dependency unavailable")
	invalid := tenantTerminal("Correct the tenant configuration", unavailable)
	complete := resolvedTenantDependencies{dependencies: &k8sops.KVServicePostgresDependencies{}, info: &platformApi.DatabaseInfo{}}
	for _, tc := range []struct {
		name         string
		db           databaseResult
		auth         error
		resolved     resolvedTenantDependencies
		reason       enterpriseApi.KVServiceTenantReason
		retry, ready bool
	}{
		{"database pending", databaseResult{}, nil, complete, enterpriseApi.KVServiceTenantDatabasePending, true, false},
		{"database API failure", databaseResult{err: unavailable}, nil, complete, enterpriseApi.KVServiceTenantDatabasePending, true, false},
		{"database conflict", databaseResult{err: invalid}, nil, complete, enterpriseApi.KVServiceTenantDatabaseConflict, false, false},
		{"authentication pending", databaseResult{ready: true}, unavailable, complete, enterpriseApi.KVServiceTenantAuthenticationPending, true, false},
		{"authentication invalid", databaseResult{ready: true}, invalid, complete, enterpriseApi.KVServiceTenantAuthenticationInvalid, false, false},
		{"missing connection references", databaseResult{ready: true}, nil, resolvedTenantDependencies{}, enterpriseApi.KVServiceTenantCredentialsPending, true, false},
		{"credential read failure", databaseResult{ready: true}, nil, resolvedTenantDependencies{err: unavailable}, enterpriseApi.KVServiceTenantCredentialsPending, true, false},
		{"ready", databaseResult{ready: true}, nil, complete, enterpriseApi.KVServiceTenantReady, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateTenant("alpha", "tenant1", tc.db, tc.auth, tc.resolved)
			require.Equal(t, tc.reason, got.status.Reason)
			require.Equal(t, tc.retry, got.retry)
			require.Equal(t, tc.ready, got.status.Ready)
			require.Equal(t, tc.ready, got.database != nil)
			require.Equal(t, "alpha-database", got.status.DatabaseName)
			if tc.auth == invalid || tc.db.err == invalid {
				require.Equal(t, "Correct the tenant configuration", got.status.Message)
				_, terminal := splcommon.TerminalMessage(got.diagnostic)
				require.True(t, terminal)
			}
		})
	}
}

func TestBuildTenantRuntimePreservesServiceDependencies(t *testing.T) {
	// Spare capacity catches an append that silently overwrites the input's backing array.
	environment := make([]corev1.EnvVar, 8)
	environment[0] = corev1.EnvVar{Name: "KVSERVICE_DB_HOST", Value: "postgres.example"}
	environment[1] = corev1.EnvVar{Name: "sentinel", Value: "unchanged"}
	beforeEnvironment := slices.Clone(environment)
	service := &k8sops.KVServicePostgresDependencies{
		Environment:             environment[:1],
		Volumes:                 []corev1.Volume{{Name: "service-volume"}},
		VolumeMounts:            []corev1.VolumeMount{{Name: "service-volume", MountPath: "/service"}},
		ConfigHash:              "service-config",
		AdminUserSecretRevision: "service-password-revision",
	}
	before := *service
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}}
	keyring := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: "tenant-verification-keys", UID: "keyring-uid", ResourceVersion: "7",
	}}
	databases := []tenantDatabase{{
		tenant: "alpha",
		dependencies: &k8sops.KVServicePostgresDependencies{
			ConfigHash: "tenant-config", AdminUserSecretRevision: "tenant-password-revision",
		},
		info: &platformApi.DatabaseInfo{AdminUserSecretRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "alpha-owner"}, Key: "password",
		}},
	}}
	ready := map[string]string{"alpha": "tenant1"}
	got, err := buildTenantRuntime(cr, ready, databases, keyring, service, "membership")
	require.NoError(t, err)
	require.False(t, service == got, "runtime dependencies must be a separate object")
	require.Equal(t, before, *service)
	require.Equal(t, beforeEnvironment, environment)
	require.Equal(t, service.Environment[0], got.Environment[0])
	require.Len(t, got.Environment, 5)
	require.Len(t, got.Volumes, 3)
	require.Len(t, got.VolumeMounts, 3)
	require.NotEqual(t, service.ConfigHash, got.ConfigHash)
	require.NotEqual(t, service.AdminUserSecretRevision, got.AdminUserSecretRevision)
	require.Equal(t, "keyring-uid:7", got.TenantKeyringRevision)

	again, err := buildTenantRuntime(cr, ready, databases, keyring, service, "membership")
	require.NoError(t, err)
	require.Equal(t, got, again, "reusing the service snapshot must produce identical dependencies")
	got.Environment[0].Value = "changed-runtime-host"
	require.Equal(t, before, *service)
}
