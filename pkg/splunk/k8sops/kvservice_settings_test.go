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

package k8sops_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig/kvservice"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func kvServiceClientFixture(t *testing.T) *spltest.MockClient {
	t.Helper()
	t.Setenv("WATCH_NAMESPACE", "")
	c := spltest.NewMockClient()
	c.AddObject(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload", Labels: map[string]string{splcommon.LabelKVService: "enabled", splcommon.LabelTenant: "customer"}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "shared.backend:8066"}}})
	_, err := splutil.ApplyNamespaceScopedSecretObject(t.Context(), c, "workload")
	require.NoError(t, err)
	c.AddObject(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: splutil.GetProbeConfigMapName("workload"), Namespace: "workload"}})
	return c
}

type kvServiceClientTestCase struct {
	name string
	cr   splcommon.MetaObject
	spec *enterpriseApi.CommonSplunkSpec
	role k8sops.InstanceType
}

func kvServiceClientCRs() []kvServiceClientTestCase {
	standalone := &enterpriseApi.Standalone{}
	indexer := &enterpriseApi.IndexerCluster{}
	searchHead := &enterpriseApi.SearchHeadCluster{}
	clusterManager := &enterpriseApi.ClusterManager{}
	licenseManager := &enterpriseApi.LicenseManager{}
	monitoringConsole := &enterpriseApi.MonitoringConsole{}
	standaloneV3 := &enterpriseApiV3.Standalone{}
	indexerV3 := &enterpriseApiV3.IndexerCluster{}
	searchHeadV3 := &enterpriseApiV3.SearchHeadCluster{}
	clusterMaster := &enterpriseApiV3.ClusterMaster{}
	licenseMaster := &enterpriseApiV3.LicenseMaster{}
	monitoringConsoleV3 := &enterpriseApiV3.MonitoringConsole{}
	return []kvServiceClientTestCase{
		{"v4 Standalone", standalone, &standalone.Spec.CommonSplunkSpec, k8sops.SplunkStandalone},
		{"v4 IndexerCluster", indexer, &indexer.Spec.CommonSplunkSpec, k8sops.SplunkIndexer},
		{"v4 SearchHeadCluster", searchHead, &searchHead.Spec.CommonSplunkSpec, k8sops.SplunkSearchHead},
		{"v4 ClusterManager", clusterManager, &clusterManager.Spec.CommonSplunkSpec, k8sops.SplunkClusterManager},
		{"v4 LicenseManager", licenseManager, &licenseManager.Spec.CommonSplunkSpec, k8sops.SplunkLicenseManager},
		{"v4 MonitoringConsole", monitoringConsole, &monitoringConsole.Spec.CommonSplunkSpec, k8sops.SplunkMonitoringConsole},
		{"v3 Standalone", standaloneV3, &standaloneV3.Spec.CommonSplunkSpec, k8sops.SplunkStandalone},
		{"v3 IndexerCluster", indexerV3, &indexerV3.Spec.CommonSplunkSpec, k8sops.SplunkIndexer},
		{"v3 SearchHeadCluster", searchHeadV3, &searchHeadV3.Spec.CommonSplunkSpec, k8sops.SplunkSearchHead},
		{"v3 ClusterMaster", clusterMaster, &clusterMaster.Spec.CommonSplunkSpec, k8sops.SplunkClusterMaster},
		{"v3 LicenseMaster", licenseMaster, &licenseMaster.Spec.CommonSplunkSpec, k8sops.SplunkLicenseMaster},
		{"v3 MonitoringConsole", monitoringConsoleV3, &monitoringConsoleV3.Spec.CommonSplunkSpec, k8sops.SplunkMonitoringConsole},
	}
}

func TestGetSplunkStatefulSetKVServiceSystemLocalDefaults(t *testing.T) {
	t.Setenv("CLUSTER_DOMAIN", "example.internal")
	for _, tc := range kvServiceClientCRs() {
		t.Run(tc.name, func(t *testing.T) {
			c := kvServiceClientFixture(t)
			tc.cr.SetNamespace("workload")
			tc.cr.SetName("client")
			tc.cr.SetUID(types.UID(tc.name))
			gv := enterpriseApi.GroupVersion
			if strings.HasPrefix(tc.name, "v3") {
				gv = enterpriseApiV3.GroupVersion
			}
			tc.cr.GetObjectKind().SetGroupVersionKind(gv.WithKind(reflect.TypeOf(tc.cr).Elem().Name()))
			defaultsConfigMap, err := configworkflow.EnsureKVServiceDefaults(t.Context(), c, tc.cr, tc.role)
			require.NoError(t, err)
			sts, err := k8sops.GetSplunkStatefulSet(t.Context(), c, tc.cr, tc.spec, tc.role, 1, nil, defaultsConfigMap.AsStatefulSetOption())
			require.NoError(t, err)
			var cmName string
			for _, v := range sts.Spec.Template.Spec.Volumes {
				if v.Name == "mnt-sok-conf-defaults" {
					require.NotNil(t, v.ConfigMap)
					cmName = v.ConfigMap.Name
				}
			}
			require.NotEmpty(t, cmName, "the active builder must mount the KVService defaults")
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(t.Context(), types.NamespacedName{Namespace: "workload", Name: cmName}, &cm))
			owner := metav1.GetControllerOf(&cm)
			require.NotNil(t, owner, "controller owner reference is required for the UID check")
			assert.Equal(t, tc.cr.GetUID(), owner.UID)
			var defaults splcommon.DefaultYML
			require.NoError(t, yaml.Unmarshal([]byte(cm.Data["conf-defaults.yml"]), &defaults))
			assert.Len(t, defaults.Splunk.Conf, 1)
			require.NotEmpty(t, defaults.Splunk.Conf, "configuration entry is required for the following checks")
			conf := defaults.Splunk.Conf[0]
			assert.Equal(t, "server", conf.ConfFileName)
			assert.Empty(t, conf.Value.Directory, "use the implicit system/local target")
			assert.Equal(t, splcommon.ConfFileStanzas{
				"kvstore": {"defaultKVStoreType": "external", "kvserviceConnectionString": "shared.backend:8066"},
				"scs":     {"tenant": "customer", "mvl-enabled": "true"},
			}, conf.Value.Stanzas)
			env := map[string]string{}
			require.NotEmpty(t, sts.Spec.Template.Spec.Containers, "container is required for environment checks")
			for _, v := range sts.Spec.Template.Spec.Containers[0].Env {
				env[v.Name] = v.Value
			}
			assert.Contains(t, env["SPLUNK_DEFAULTS_URL"], "/mnt/sok-defaults/conf-defaults.yml")
			for _, name := range []string{"SPLUNK_KVSTORE_DEFAULT_TYPE", "SPLUNK_KVSERVICE_MODE", "SPLUNK_KVSERVICE_SCOPE", "SPLUNK_SCS_TENANT", "SPLUNK_SCS_ENVIRONMENT"} {
				assert.NotContains(t, env, name)
			}
		})
	}
}

func TestGetKVServiceSettingsNamespaceMetadata(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	for _, tc := range []struct {
		name, enabled, address, tenant string
		found                          bool
		wantError                      string
	}{
		{name: "enabled", enabled: "enabled", address: "shared.backend:8066", tenant: "customer", found: true},
		{name: "tenant label is optional", enabled: "enabled", address: "shared.backend:8066", found: true},
		{name: "disabled", enabled: "disabled", address: "shared.backend:8066"},
		{name: "absent", address: "shared.backend:8066"},
		{name: "case sensitive", enabled: "Enabled", address: "shared.backend:8066"},
		{name: "disabled ignores missing address", enabled: "disabled"},
		{name: "missing address", enabled: "enabled", wantError: "annotation platform.splunk.com/kvservice-addr must not be empty"},
		{name: "custom port", enabled: "enabled", address: "shared.backend:9000", found: true},
		{name: "no port added", enabled: "enabled", address: "shared", found: true},
		{name: "scheme preserved", enabled: "enabled", address: "https://shared.backend:9000", found: true},
		{name: "fqdn preserved", enabled: "enabled", address: "shared.backend.svc.cluster.local:8066", found: true},
		{name: "ipv6 preserved", enabled: "enabled", address: "[2001:db8::1]:8066", found: true},
		{name: "no normalization", enabled: "enabled", address: " supplied address ", found: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := spltest.NewMockClient()
			c.AddObject(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload", Labels: map[string]string{splcommon.LabelKVService: tc.enabled, splcommon.LabelTenant: tc.tenant}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: tc.address}}})
			cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Namespace: "workload"}}
			settings, found, err := k8sops.GetKVServiceSettings(t.Context(), c, cr, k8sops.SplunkStandalone)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.False(t, found)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.found, found)
			if found {
				assert.Equal(t, k8sops.KVServiceSettings{ConnectionString: tc.address, Tenant: func() string {
					if tc.tenant != "" {
						return tc.tenant
					}
					return "workload"
				}()}, settings)
			}
		})
	}
}

func TestGetKVServiceSettingsErrorsAndExcludedPods(t *testing.T) {
	c := kvServiceClientFixture(t)
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Namespace: "workload"}}
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = errors.New("namespace access denied")
	_, found, err := k8sops.GetKVServiceSettings(t.Context(), c, cr, k8sops.SplunkStandalone)
	assert.False(t, found)
	require.ErrorContains(t, err, "namespace access denied")
	// Excluded roles never read namespace settings, even if the API is unavailable.
	_, found, err = k8sops.GetKVServiceSettings(t.Context(), c, &enterpriseApi.SearchHeadCluster{}, k8sops.SplunkDeployer)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = k8sops.GetKVServiceSettings(t.Context(), c, &enterpriseApi.IngestorCluster{}, k8sops.SplunkIngestor)
	require.NoError(t, err)
	assert.False(t, found)
	delete(c.InduceErrorKind, splcommon.MockClientInduceErrorGet)
	cr.Namespace = "missing"
	_, found, err = k8sops.GetKVServiceSettings(t.Context(), c, cr, k8sops.SplunkStandalone)
	require.ErrorContains(t, err, "namespace \"missing\"")
	assert.False(t, found)
	t.Setenv("WATCH_NAMESPACE", "elsewhere")
	cr.Namespace = "workload"
	_, found, err = k8sops.GetKVServiceSettings(t.Context(), c, cr, k8sops.SplunkStandalone)
	require.ErrorContains(t, err, "not watched")
	assert.False(t, found)
}

func TestKVServiceDefaultsUpdateAndOwnership(t *testing.T) {
	c := kvServiceClientFixture(t)
	cr := kvServiceClientCRs()[0].cr
	cr.SetName("client")
	cr.SetNamespace("workload")
	cr.SetUID("first")
	cr.GetObjectKind().SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("Standalone"))
	initial, err := configworkflow.EnsureKVServiceDefaults(t.Context(), c, cr, k8sops.SplunkStandalone)
	require.NoError(t, err)
	require.NotNil(t, initial.Immutable)
	assert.True(t, *initial.Immutable)
	set, err := splunkconfig.ForRole(k8sops.SplunkStandalone).
		NonSensitiveConfDir("").
		With(kvservice.Conf("changed.backend:8066", "customer")).Build()
	require.NoError(t, err)
	owner := splcommon.AsOwner(cr, true)
	changed, err := configworkflow.EnsureConfigMap(t.Context(), c, cr, set.NonSensitive, &owner)
	require.NoError(t, err)
	assert.NotEqual(t, initial.Name, changed.Name)
	cr.SetUID("another")
	owner = splcommon.AsOwner(cr, true)
	_, err = configworkflow.EnsureConfigMap(context.Background(), c, cr, set.NonSensitive, &owner)
	assert.ErrorContains(t, err, "is not owned by client")
}

func TestGetSplunkStatefulSetWithoutKVServiceKeepsLocalDefaults(t *testing.T) {
	c := kvServiceClientFixture(t)
	c.AddObject(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload"}})
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "workload"}}
	defaults, err := configworkflow.EnsureKVServiceDefaults(t.Context(), c, cr, k8sops.SplunkStandalone)
	require.NoError(t, err)
	sts, err := k8sops.GetSplunkStatefulSet(t.Context(), c, cr, &cr.Spec.CommonSplunkSpec, k8sops.SplunkStandalone, 1, nil, defaults.AsStatefulSetOption())
	require.NoError(t, err)
	env := map[string]string{}
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers, "container is required for environment checks")
	for _, v := range sts.Spec.Template.Spec.Containers[0].Env {
		env[v.Name] = v.Value
	}
	assert.NotContains(t, env, "SPLUNK_KVSTORE_DEFAULT_TYPE")
	assert.Contains(t, env["SPLUNK_DEFAULTS_URL"], "/mnt/sok-defaults/conf-defaults.yml")
	var cm corev1.ConfigMap
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Namespace: "workload", Name: defaults.Name}, &cm))
	var data splcommon.DefaultYML
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["conf-defaults.yml"]), &data))
	require.Len(t, data.Splunk.Conf, 1)
	assert.Equal(t, splcommon.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "local", "postgresMigrateOnStartup": "false"}}, data.Splunk.Conf[0].Value.Stanzas)
}

func TestNamespaceKVServiceChangesUpdateAndRestoreLocalPodDefaults(t *testing.T) {
	c := kvServiceClientFixture(t)
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: "workload", UID: "consumer"},
	}
	build := func() (*appsv1.StatefulSet, string) {
		t.Helper()
		defaults, err := configworkflow.EnsureKVServiceDefaults(t.Context(), c, cr, k8sops.SplunkStandalone)
		require.NoError(t, err)
		sts, err := k8sops.GetSplunkStatefulSet(t.Context(), c, cr, &cr.Spec.CommonSplunkSpec, k8sops.SplunkStandalone, 1, nil, defaults.AsStatefulSetOption())
		require.NoError(t, err)
		return sts, defaults.Name
	}
	initial, initialName := build()
	var namespace corev1.Namespace
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "workload"}, &namespace))
	namespace.Annotations[splcommon.AnnotationKVServiceAddr] = "replacement.platform:9000"
	c.AddObject(&namespace)
	changed, addressName := build()
	assert.NotEqual(t, initialName, addressName)
	assert.NotEqual(t, initial.Spec.Template, changed.Spec.Template)
	namespace.Labels[splcommon.LabelTenant] = "another-tenant"
	c.AddObject(&namespace)
	_, tenantName := build()
	assert.NotEqual(t, addressName, tenantName, "tenant changes update client defaults")
	delete(namespace.Labels, splcommon.LabelKVService)
	c.AddObject(&namespace)
	disabled, disabledName := build()
	assert.NotEmpty(t, disabledName)
	assert.NotEqual(t, addressName, disabledName)
	var cm corev1.ConfigMap
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Namespace: "workload", Name: disabledName}, &cm))
	var data splcommon.DefaultYML
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["conf-defaults.yml"]), &data))
	require.Len(t, data.Splunk.Conf, 1)
	assert.Equal(t, splcommon.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "local", "postgresMigrateOnStartup": "false"}}, data.Splunk.Conf[0].Value.Stanzas)
	env := map[string]string{}
	for _, item := range disabled.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item.Value
	}
	assert.NotContains(t, env, "SPLUNK_KVSTORE_DEFAULT_TYPE")
	assert.Contains(t, env["SPLUNK_DEFAULTS_URL"], "/mnt/sok-defaults/")
}
