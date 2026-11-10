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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type runtimeClassCR struct {
	name string
	cr   splcommon.MetaObject
	spec *enterpriseApi.CommonSplunkSpec
	role k8sops.InstanceType
}

func runtimeClassCRs() []runtimeClassCR {
	ingestor := &enterpriseApi.IngestorCluster{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	standalone := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	indexer := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	searchHead := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	clusterManager := &enterpriseApi.ClusterManager{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	licenseManager := &enterpriseApi.LicenseManager{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}
	monitoringConsole := &enterpriseApi.MonitoringConsole{ObjectMeta: metav1.ObjectMeta{Name: "rc", Namespace: "workload"}}

	return []runtimeClassCR{
		{"IngestorCluster", ingestor, &ingestor.Spec.CommonSplunkSpec, k8sops.SplunkIngestor},
		{"Standalone", standalone, &standalone.Spec.CommonSplunkSpec, k8sops.SplunkStandalone},
		{"IndexerCluster", indexer, &indexer.Spec.CommonSplunkSpec, k8sops.SplunkIndexer},
		{"SearchHeadCluster", searchHead, &searchHead.Spec.CommonSplunkSpec, k8sops.SplunkSearchHead},
		{"ClusterManager", clusterManager, &clusterManager.Spec.CommonSplunkSpec, k8sops.SplunkClusterManager},
		{"LicenseManager", licenseManager, &licenseManager.Spec.CommonSplunkSpec, k8sops.SplunkLicenseManager},
		{"MonitoringConsole", monitoringConsole, &monitoringConsole.Spec.CommonSplunkSpec, k8sops.SplunkMonitoringConsole},
	}
}

// A RuntimeClassName set on the CR spec must reach the StatefulSet pod template so
// kubelet runs the pods under that runtime.
func TestGetSplunkStatefulSetSetsRuntimeClassName(t *testing.T) {
	for _, tc := range runtimeClassCRs() {
		t.Run(tc.name, func(t *testing.T) {
			c := kvServiceClientFixture(t)
			tc.spec.RuntimeClassName = ptr.To("isolated-sandbox")

			sts, err := k8sops.GetSplunkStatefulSet(t.Context(), c, tc.cr, tc.spec, tc.role, 1, nil)
			require.NoError(t, err)

			got := sts.Spec.Template.Spec.RuntimeClassName
			require.NotNil(t, got, "RuntimeClassName must be set on the pod template")
			assert.Equal(t, "isolated-sandbox", *got)
		})
	}
}

// An unset RuntimeClassName must leave the pod template field nil rather than an empty
// string, so the cluster's default runtime handler is used and no pod churn occurs.
func TestGetSplunkStatefulSetLeavesRuntimeClassNameUnset(t *testing.T) {
	for _, tc := range runtimeClassCRs() {
		t.Run(tc.name, func(t *testing.T) {
			c := kvServiceClientFixture(t)

			sts, err := k8sops.GetSplunkStatefulSet(t.Context(), c, tc.cr, tc.spec, tc.role, 1, nil)
			require.NoError(t, err)

			assert.Nil(t, sts.Spec.Template.Spec.RuntimeClassName)
		})
	}
}
