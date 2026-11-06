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

package k8sops

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDiscoverKVServiceTenants(t *testing.T) {
	for _, test := range []struct {
		name, watch string
		modify      func(*corev1.Namespace)
		want        map[string]string
		err         string
	}{
		{name: "label override", want: map[string]string{"alpha": "tenant1"}},
		{name: "namespace fallback", modify: func(ns *corev1.Namespace) { delete(ns.Labels, splcommon.LabelTenant) }, want: map[string]string{"tenant1": "tenant1"}},
		{name: "disabled", modify: func(ns *corev1.Namespace) { ns.Labels[splcommon.LabelKVService] = "true" }, want: map[string]string{}},
		{name: "other backend", modify: func(ns *corev1.Namespace) {
			ns.Annotations[splcommon.AnnotationKVServiceAddr] = "splunk-other-kvservice-service.shared:8066"
		}, want: map[string]string{}},
		{name: "missing address", modify: func(ns *corev1.Namespace) { ns.Annotations = nil }, want: map[string]string{}},
		{name: "empty address", modify: func(ns *corev1.Namespace) { ns.Annotations[splcommon.AnnotationKVServiceAddr] = "" }, want: map[string]string{}},
		{name: "obsolete address label ignored", modify: func(ns *corev1.Namespace) { ns.Labels[splcommon.AnnotationKVServiceAddr] = "other.shared" }, want: map[string]string{"alpha": "tenant1"}},
		{name: "obsolete address label alone", modify: func(ns *corev1.Namespace) {
			ns.Labels[splcommon.AnnotationKVServiceAddr] = "splunk-kvs-kvservice-service.shared"
			ns.Annotations = nil
		}, want: map[string]string{}},
		{name: "outside scope", watch: "shared,other", want: map[string]string{}},
		{name: "within scope", watch: "shared,tenant1", want: map[string]string{"alpha": "tenant1"}},
		{name: "terminating", modify: func(ns *corev1.Namespace) {
			now := metav1.Now()
			ns.DeletionTimestamp = &now
			ns.Finalizers = []string{"test"}
		}, want: map[string]string{}},
		{name: "invalid database name", modify: func(ns *corev1.Namespace) { ns.Labels[splcommon.LabelTenant] = "bad-tenant" }, err: "invalid tenant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WATCH_NAMESPACE", test.watch)
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant1", Labels: map[string]string{
				splcommon.LabelKVService: "enabled", splcommon.LabelTenant: "alpha",
			}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs-kvservice-service.shared:8066"}}}
			if test.modify != nil {
				test.modify(ns)
			}
			// An unrelated ConfigMap must never influence namespace discovery.
			unrelatedMapping := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "tenant-map", Namespace: "shared"}, Data: map[string]string{"stale": "old"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, unrelatedMapping).Build()
			cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}}
			mapping, err := DiscoverKVServiceTenants(context.Background(), c, cr)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, mapping)
		})
	}
}

func TestDiscoverKVServiceRejectsDuplicateTenants(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	var objects []client.Object
	for _, name := range []string{"a", "b"} {
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			splcommon.LabelKVService: "enabled", splcommon.LabelTenant: "duplicate",
		}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs-kvservice-service.shared:8066"}}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}}
	_, err := DiscoverKVServiceTenants(context.Background(), c, cr)
	require.ErrorContains(t, err, "both namespaces")
	_, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
}

type tenantListFailure struct{ client.Client }

func (c tenantListFailure) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("namespace list unavailable")
}

func TestDiscoverKVServiceDoesNotTreatReadFailureAsEmptyMembership(t *testing.T) {
	_, err := DiscoverKVServiceTenants(context.Background(), tenantListFailure{}, &enterpriseApi.KVService{})
	require.ErrorContains(t, err, "namespace list unavailable")
}

func TestKVServiceRuntimeConfigOwnershipAndRepair(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared", UID: "owner"}}
	mapping := map[string]string{"alpha": "tenant1"}
	config, err := ApplyKVServiceTenantConfig(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.Equal(t, "splunk-kvs-kvservice-tenants", config.Name)
	require.Equal(t, mapping, config.Data)
	require.Equal(t, cr.UID, metav1.GetControllerOf(config).UID)
	config.Data["alpha"] = "tampered"
	require.NoError(t, c.Update(ctx, config))
	config, err = ApplyKVServiceTenantConfig(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.Equal(t, mapping, config.Data)
	config.OwnerReferences = nil
	require.NoError(t, c.Update(ctx, config))
	_, err = ApplyKVServiceTenantConfig(ctx, c, cr, mapping)
	require.ErrorContains(t, err, "not owned")
}

func TestInvalidUnselectedAddressDoesNotSuspendOtherTenantNamespaces(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	selected := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant1", Labels: map[string]string{splcommon.LabelKVService: "enabled"}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs-kvservice-service.shared:8066"}}}
	invalid := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Labels: map[string]string{splcommon.LabelKVService: "enabled"}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "https://"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(selected, invalid).Build()
	mapping, err := DiscoverKVServiceTenants(t.Context(), c, &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"tenant1": "tenant1"}, mapping)
}

func TestTenantDiscoveryMessageIncludesNamespaceAndRepair(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-one", Labels: map[string]string{splcommon.LabelKVService: "enabled", splcommon.LabelTenant: "invalid-tenant"}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs-kvservice-service.shared:8066"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
	_, err := DiscoverKVServiceTenants(t.Context(), c, &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}})
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	require.Contains(t, message, "tenant-one")
	require.Contains(t, message, "Set label platform.splunk.com/tenant")
	require.Contains(t, message, "^[a-z][a-z0-9]{0,29}$")
	var validation *splcommon.TerminalError
	require.ErrorAs(t, err, &validation)
	require.NotNil(t, validation.Err)
}

func TestKVServiceAddressContractAcrossConsumers(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	for _, tc := range []struct {
		name, namespace, address string
		allowed                  bool
	}{
		{"shared", "default", "splunk-shared-kvservice-service.default:8066", true},
		{"dedicated", "tenant1", "https://splunk-dedicated-kvservice-service.tenant1:8066", true},
		{"shared", "default", "splunk-shared-kvservice-service.default.svc.cluster.local:8066", false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload", Labels: map[string]string{
				splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: "tenant1",
			}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: tc.address}}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
			_, found, err := GetKVServiceSettings(t.Context(), c, &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name}}, SplunkStandalone)
			require.Equal(t, tc.allowed, found)
			require.Equal(t, tc.allowed, err == nil, "settings error: %v", err)
			mapping, err := DiscoverKVServiceTenants(t.Context(), c, &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: tc.namespace}})
			require.NoError(t, err)
			mapper := KVServicePostgresMapper{Client: c}
			requests := mapper.MapTenantNamespaceSecret(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: splcommon.GetNamespaceScopedSecretName(ns.Name)}})
			if tc.allowed {
				require.Equal(t, map[string]string{"tenant1": ns.Name}, mapping)
				require.Len(t, requests, 1)
				require.Equal(t, tc.name, requests[0].Name)
				require.Equal(t, tc.namespace, requests[0].Namespace)
			} else {
				require.Empty(t, mapping)
				require.Empty(t, requests)
			}
		})
	}
}

func TestInvalidKVServiceAddressLogIsActionableAndSanitized(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload", Annotations: map[string]string{
		splcommon.AnnotationKVServiceAddr: "https://api-user:do-not-log-this@splunk-shared-kvservice-service.default:8066",
	}}}
	var output bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)))
	_, err := namespaceKVServiceAddress(ctx, ns)
	require.Error(t, err)
	require.Contains(t, output.String(), ns.Name)
	require.Contains(t, output.String(), splcommon.AnnotationKVServiceAddr)
	require.NotContains(t, output.String(), "do-not-log-this")
	require.NotContains(t, output.String(), "api-user")
}
