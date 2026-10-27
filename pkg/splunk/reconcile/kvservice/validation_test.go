// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateKVServiceSpec(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	err := validateKVServiceSpec(ctx, c, cr)
	if err != nil {
		t.Errorf("validateKVServiceSpec error=%v", err)
	}
	if cr.Spec.Replicas != 1 {
		t.Errorf("replicas=%d; want 1", cr.Spec.Replicas)
	}
	if cr.Spec.Image != "kvstore/kvservice:latest" {
		t.Errorf("image=%s; want %s", cr.Spec.Image, "kvstore/kvservice:latest")
	}
	if cr.Spec.Volumes == nil {
		t.Errorf("volumes should be non-nil")
	}
	if len(cr.Spec.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets len=%d; want 0", len(cr.Spec.ImagePullSecrets))
	}

	cr.Spec.ImagePullPolicy = "invalid"
	err = validateKVServiceSpec(ctx, c, cr)
	if err == nil {
		t.Error("invalid image pull policy must fail validation")
	}
	cr.Spec.ImagePullPolicy = "IfNotPresent"
	for name, spec := range map[string]enterpriseApi.KVServiceSpec{
		"liveness delay":   {CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{LivenessInitialDelaySeconds: -1}},
		"readiness delay":  {CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{ReadinessInitialDelaySeconds: -1}},
		"liveness timing":  {LivenessProbe: &enterpriseApi.Probe{TimeoutSeconds: -1}},
		"readiness timing": {ReadinessProbe: &enterpriseApi.Probe{PeriodSeconds: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			spec.Spec = cr.Spec.Spec
			invalid := &enterpriseApi.KVService{ObjectMeta: cr.ObjectMeta, Spec: spec}
			if err := validateKVServiceSpec(ctx, c, invalid); err == nil {
				t.Error("negative probe timing must fail validation")
			}
		})
	}
	for _, name := range []string{
		"KVSERVICE_SINGLE_TENANT_K8S_MODE",
		"KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY",
		"KVSERVICE_DB_HOST",
		"KVSERVICE_DB_PORT",
		"KVSERVICE_SINGLE_TENANT_PRIMARY_HOST",
		"KVSERVICE_SINGLE_TENANT_PRIMARY_PORT",
		"KVSERVICE_SINGLE_TENANT_REPLICA_HOST",
		"KVSERVICE_SINGLE_TENANT_REPLICA_PORT",
		"KVSERVICE_DB_ROOT_USER",
		"KVSERVICE_DB_SERVICE_USER",
		"KVSERVICE_DB_PASSWORD",
		"KVSERVICE_DB_SERVICE_PASSWORD",
	} {
		t.Run("reserved "+name, func(t *testing.T) {
			candidate := cr.DeepCopy()
			candidate.Spec.Postgres = &enterpriseApi.KVServicePostgresSpec{}
			candidate.Spec.ExtraEnv = []corev1.EnvVar{{Name: name, Value: "override"}}
			require.ErrorContains(t, validateKVServiceSpec(t.Context(), c, candidate), name)
			candidate.Spec.Postgres = nil
			require.NoError(t, validateKVServiceSpec(t.Context(), c, candidate), "without PostgreSQL configuration the variable is user-managed")
		})
	}
	t.Run("unrelated extra environment", func(t *testing.T) {
		candidate := cr.DeepCopy()
		candidate.Spec.Postgres = &enterpriseApi.KVServicePostgresSpec{}
		candidate.Spec.ExtraEnv = []corev1.EnvVar{{Name: "CUSTOM_SETTING", Value: "allowed"}}
		require.NoError(t, validateKVServiceSpec(t.Context(), c, candidate))
	})
}

func TestValidateKVServiceCertsFeatureGate(t *testing.T) {
	previous := config.DefaultMutableFeatureGate.Enabled(config.CertManagement)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): false}))
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): previous}))
	})
	cr := &enterpriseApi.KVService{}
	cr.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "postgres-ca"}}}
	err := validateKVServiceSpec(context.Background(), spltest.NewMockClient(), cr)
	require.ErrorContains(t, err, "CertManagement")
}
