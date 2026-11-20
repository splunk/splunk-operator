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

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestKVServiceAdmissionManagedEnvironment(t *testing.T) {
	for _, name := range []string{"KVSERVICE_CMPK_TENANT_MAP_DIR", "KVSERVICE_CMPK_TENANT_VERIFICATION_KEYS_DIR", "KVSERVICE_DB_PASSWORD", "KVSERVICE_DB_SERVICE_PASSWORD"} {
		obj := &enterpriseApi.KVService{Spec: enterpriseApi.KVServiceSpec{
			CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{ExtraEnv: []corev1.EnvVar{{Name: name, Value: "override"}}},
			Postgres:           enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}},
		}}
		require.NotEmpty(t, ValidateKVServiceUpdate(obj, obj))
	}
}

func TestKVServiceAdmissionReservesTLSOverridesOnlyWithHTTPS(t *testing.T) {
	oldKV, oldPG, oldCert := config.DefaultMutableFeatureGate.Enabled(config.KVService), config.DefaultMutableFeatureGate.Enabled(config.PostgresController), config.DefaultMutableFeatureGate.Enabled(config.CertManagement)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.KVService): true, string(config.PostgresController): true, string(config.CertManagement): true}))
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.KVService): oldKV, string(config.PostgresController): oldPG, string(config.CertManagement): oldCert}))
	})
	for _, name := range []string{"TLS_CERTIFICATE", "TLS_PRIVATE_KEY", "TLS_HOST", "TLS_PORT"} {
		t.Run(name, func(t *testing.T) {
			for _, withCerts := range []bool{false, true} {
				obj := &enterpriseApi.KVService{Spec: enterpriseApi.KVServiceSpec{
					CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{ExtraEnv: []corev1.EnvVar{{Name: name, Value: "override"}}},
					Postgres:           enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}},
				}}
				if withCerts {
					obj.Spec.Scheme = "https"
					obj.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "tls"}}}
				}
				createErrs := ValidateKVServiceCreate(obj)
				updateErrs := ValidateKVServiceUpdate(obj, obj)
				if withCerts {
					require.ErrorContains(t, createErrs.ToAggregate(), "spec.extraEnv[0].name")
					require.ErrorContains(t, updateErrs.ToAggregate(), "is managed by the operator")
				} else {
					require.Empty(t, createErrs)
					require.Empty(t, updateErrs)
				}
			}
		})
	}
}

func TestKVServiceAdmissionProbeOverrides(t *testing.T) {
	for _, test := range []struct {
		name  string
		probe *enterpriseApi.Probe
		valid bool
	}{
		{"omitted", nil, true},
		{"defaults", &enterpriseApi.Probe{}, true},
		{"partial", &enterpriseApi.Probe{InitialDelaySeconds: 60}, true},
		{"explicit", &enterpriseApi.Probe{TimeoutSeconds: 2, PeriodSeconds: 10, FailureThreshold: 3}, true},
		{"negative delay", &enterpriseApi.Probe{InitialDelaySeconds: -1}, false},
		{"negative timeout", &enterpriseApi.Probe{TimeoutSeconds: -1}, false},
		{"negative period", &enterpriseApi.Probe{PeriodSeconds: -1}, false},
		{"negative threshold", &enterpriseApi.Probe{FailureThreshold: -1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			obj := &enterpriseApi.KVService{}
			obj.Spec.LivenessProbe, obj.Spec.ReadinessProbe = test.probe, test.probe
			original := obj.DeepCopy()
			errs := validateKVServiceSpec(obj)
			require.Equal(t, test.valid, len(errs) == 0, "%v", errs)
			require.Equal(t, original, obj, "admission must not mutate probe configuration")
		})
	}
}

func TestKVServiceAdmissionAllowsTerminatingCleanupWithoutSpecChange(t *testing.T) {
	previous := config.DefaultMutableFeatureGate.Enabled(config.CertManagement)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): false}))
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): previous}))
	})
	old := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"cleanup"}}}
	old.Spec.Scheme = "https"
	old.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "tls"}}}
	obj := old.DeepCopy()
	now := metav1.Now()
	obj.DeletionTimestamp = &now
	obj.Finalizers = nil
	require.Empty(t, ValidateKVServiceUpdate(obj, old))
	obj.Spec.Image = "changed"
	require.NotEmpty(t, ValidateKVServiceUpdate(obj, old), "terminating spec edits still require validation")
	obj = old.DeepCopy()
	obj.Finalizers = nil
	require.NotEmpty(t, ValidateKVServiceUpdate(obj, old), "non-terminating edits still require validation")
}

func TestKVServiceAdmissionCertificateNames(t *testing.T) {
	for _, name := range []string{"", "Bad_Name"} {
		t.Run(name, func(t *testing.T) {
			obj := &enterpriseApi.KVService{Spec: enterpriseApi.KVServiceSpec{Scheme: "https"}}
			obj.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: name}}}
			require.ErrorContains(t, validateKVServiceSpec(obj).ToAggregate(), "spec.certs[0].secretRef.name")
			obj.Spec.Scheme = "http"
			original := obj.DeepCopy()
			require.Empty(t, validateKVServiceSpec(obj), "inactive certificate references must remain ignored")
			require.Equal(t, original, obj, "validation must not discard the submitted certificates")
		})
	}
}

func TestKVServiceAdmissionFeatureGate(t *testing.T) {
	oldKV, oldPG := config.DefaultMutableFeatureGate.Enabled(config.KVService), config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.KVService): oldKV, string(config.PostgresController): oldPG}))
	})
	obj := &enterpriseApi.KVService{TypeMeta: metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}, Spec: enterpriseApi.KVServiceSpec{Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	for _, kvEnabled := range []bool{false, true} {
		for _, pgEnabled := range []bool{false, true} {
			require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.KVService): kvEnabled, string(config.PostgresController): pgEnabled}))
			for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
				request := &admissionv1.AdmissionReview{Request: &admissionv1.AdmissionRequest{Operation: operation, Namespace: obj.Namespace,
					Resource: metav1.GroupVersionResource{Group: enterpriseApi.GroupVersion.Group, Version: enterpriseApi.GroupVersion.Version, Resource: "kvservices"},
					Object:   runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}}
				_, err := Validate(request, DefaultValidators)
				if operation == admissionv1.Create && (!kvEnabled || !pgEnabled) {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}
		}
	}
}

func TestKVServiceSingletonAdmission(t *testing.T) {
	featuregatetesting.SetFeatureGatesDuringTest(t, config.DefaultMutableFeatureGate, featuregatetesting.FeatureOverrides{
		config.KVService: true, config.PostgresController: true,
	})
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	obj := &enterpriseApi.KVService{TypeMeta: metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "shared"}, Spec: enterpriseApi.KVServiceSpec{Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
	require.Error(t, ValidateKVServiceCreateWithContext(obj, nil).ToAggregate())
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, namespace      string
		terminating, allowed bool
		listFailure          bool
	}{
		{name: "first service", allowed: true},
		{name: "different hosting namespace", namespace: "elsewhere", allowed: true},
		{name: "duplicate", namespace: "shared"},
		{name: "deleting predecessor", namespace: "shared", terminating: true},
		{name: "API unavailable", listFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cached := fake.NewClientBuilder().WithScheme(scheme).Build()
			live := fake.NewClientBuilder().WithScheme(scheme).Build()
			if tc.namespace != "" {
				existing := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "existing", Namespace: tc.namespace}}
				if tc.terminating {
					now := metav1.Now()
					existing.DeletionTimestamp, existing.Finalizers = &now, []string{"test/pending-cleanup"}
				}
				require.NoError(t, live.Create(t.Context(), existing))
			}
			// The admission reader must see the live object even if the manager cache is empty.
			if tc.listFailure {
				live = interceptor.NewClient(live, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return errors.New("injected API failure")
				}})
			}
			c := NewDirectReadClient(cached, live)
			dryRun := true
			for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
				request := &admissionv1.AdmissionReview{Request: &admissionv1.AdmissionRequest{Operation: operation, Namespace: obj.Namespace, DryRun: &dryRun,
					Resource: metav1.GroupVersionResource{Group: KVServiceGVR.Group, Version: KVServiceGVR.Version, Resource: KVServiceGVR.Resource},
					Object:   runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}}
				_, err := ValidateWithClient(request, DefaultValidators, c)
				if operation == admissionv1.Create && tc.listFailure {
					require.ErrorContains(t, err, "injected API failure")
				} else if operation == admissionv1.Create && !tc.allowed {
					require.ErrorContains(t, err, "only one KVService is allowed")
					require.ErrorContains(t, err, "existing")
				} else {
					require.NoError(t, err, "updates/deletion must remain available to resolve a conflict")
				}
			}
		})
	}
}
