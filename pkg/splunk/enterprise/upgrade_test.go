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

package enterprise_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	indexercluster "github.com/splunk/splunk-operator/pkg/splunk/reconcile/indexercluster"
	reconcile "github.com/splunk/splunk-operator/pkg/splunk/reconcile/licensemanager"
	searchheadcluster "github.com/splunk/splunk-operator/pkg/splunk/reconcile/searchheadcluster"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func init() {
	// Re-assign probe script locations for tests running from the enterprise package directory.
	splutil.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../tools/k8_probes/readinessProbe.sh")
		return fileLocation
	}
	splutil.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../tools/k8_probes/livenessProbe.sh")
		return fileLocation
	}
	splutil.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../tools/k8_probes/startupProbe.sh")
		return fileLocation
	}
}

func TestUpgradePathValidation(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	client := builder.Build()
	ctx := context.TODO()

	oldVerifyRFPeersCall := indexercluster.VerifyRFPeersCall
	oldGetClusterInfoForUpgradeCall := indexercluster.GetClusterInfoForUpgradeCall
	oldGetClusterManagerInfoForReconcileCall := indexercluster.GetClusterManagerInfoForReconcileCall
	oldGetClusterManagerPeersForReconcileCall := indexercluster.GetClusterManagerPeersForReconcileCall
	defer func() {
		indexercluster.VerifyRFPeersCall = oldVerifyRFPeersCall
		indexercluster.GetClusterInfoForUpgradeCall = oldGetClusterInfoForUpgradeCall
		indexercluster.GetClusterManagerInfoForReconcileCall = oldGetClusterManagerInfoForReconcileCall
		indexercluster.GetClusterManagerPeersForReconcileCall = oldGetClusterManagerPeersForReconcileCall
	}()
	indexercluster.VerifyRFPeersCall = func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) error {
		return nil
	}
	indexercluster.GetClusterInfoForUpgradeCall = func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (*splclient.ClusterInfo, error) {
		return &splclient.ClusterInfo{}, nil
	}
	indexercluster.GetClusterManagerInfoForReconcileCall = func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (*splclient.ClusterManagerInfo, error) {
		return &splclient.ClusterManagerInfo{Initialized: true, IndexingReady: true, ServiceReady: true}, nil
	}
	indexercluster.GetClusterManagerPeersForReconcileCall = func(_ context.Context, _ splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (map[string]splclient.ClusterManagerPeerInfo, error) {
		return map[string]splclient.ClusterManagerPeerInfo{
			fmt.Sprintf("splunk-%s-indexer-0", cr.Name): {ID: "peer-0", Status: "Up", Searchable: true},
		}, nil
	}

	stdln := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	err := client.Create(ctx, &stdln)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}

	// cluster manager

	lm := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	mc := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test",
				},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	idx := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test",
				},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:old",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test",
				},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	err = client.Create(ctx, &lm)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}
	err = client.Create(ctx, &cm)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}
	err = client.Create(ctx, &mc)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}
	err = client.Create(ctx, &idx)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}
	err = client.Create(ctx, &shc)
	if err != nil {
		t.Errorf("create should not have returned error; err=%v", err)
	}

	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManagershould not have returned error; err=%v", err)
	}

	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}

	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	// license manager statefulset is not created
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create license manager statefulset
	_, err = reconcile.ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error; err=%v", err)
	}

	// create pods for license manager
	spltest.CreatePods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true
	// create license manager statefulset
	_, err = reconcile.ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	// cluster manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	// cluster manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManagershould not have returned error; err=%v", err)
	}

	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	// cluster manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}

	namespacedName := types.NamespacedName{
		Name:      "test",
		Namespace: "test",
	}
	err = client.Get(ctx, namespacedName, &lm)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	if lm.Status.Phase != enterpriseApi.PhaseReady {
		t.Errorf("lm is not in ready state")
	}

	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	// lm statefulset should have been created by now, this should pass
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "cluster-manager", fmt.Sprintf("splunk-%s-cluster-manager-0", cm.Name), cm.Namespace, cm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-cluster-manager", cm.Name), cm.Namespace)
	cm.Status.TelAppInstalled = true
	// cluster manager is found  and creat
	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	// lm statefulset should have been created by now, this should pass
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	err = client.Get(ctx, namespacedName, &cm)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	if cm.Status.Phase != enterpriseApi.PhaseReady {
		t.Errorf("cm is not in ready state")
	}

	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	// monitoring console statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	// monitoring console statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	// Monitoring console is ready now, now this should crete statefulset but statefulset is not in ready phase
	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-0", shc.Name), shc.Namespace, shc.Spec.Image)
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-1", shc.Name), shc.Namespace, shc.Spec.Image)
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-2", shc.Name), shc.Namespace, shc.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 3, fmt.Sprintf("splunk-%s-search-head", shc.Name), shc.Namespace)
	spltest.CreatePods(t, ctx, client, "deployer", fmt.Sprintf("splunk-%s-deployer-0", shc.Name), shc.Namespace, shc.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-deployer", shc.Name), shc.Namespace)

	// used in mocking this function
	shcworkflow.GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *shcworkflow.PodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
		shcm := &splclient.SearchHeadClusterMemberInfo{
			Status: "Up",
		}
		return shcm, nil
	}

	// used in mocking this function
	shcworkflow.GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *shcworkflow.PodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
		shci := &splclient.SearchHeadCaptainInfo{
			ServiceReady: true,
			Initialized:  true,
		}
		return shci, nil
	}
	// Now SearchheadCluster should move to READY state
	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	err = client.Get(ctx, namespacedName, &shc)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	if shc.Status.Phase != enterpriseApi.PhaseReady {
		t.Errorf("shc is not in ready state")
	}

	// search head cluster is ready, this should create statefulset but they are not ready
	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	// create pods for indexer cluster
	spltest.CreatePods(t, ctx, client, "indexer", fmt.Sprintf("splunk-%s-indexer-0", idx.Name), idx.Namespace, idx.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-indexer", idx.Name), idx.Namespace)

	// search head cluster is not ready, so wait for search head cluster
	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	err = client.Get(ctx, namespacedName, &idx)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	if idx.Status.Phase != enterpriseApi.PhaseReady {
		t.Errorf("shc is not in ready state")
	}

	enterprise.GetCMMultisiteEnvVarsCall = func(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := []corev1.EnvVar{{
			Name:  splcommon.ClusterManagerURL,
			Value: splcommon.GetSplunkServiceName(splcommon.SplunkClusterManager, cr.GetName(), false),
		}}
		return extraEnv, nil
	}

	// mointoring console statefulset is created here
	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}
	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "monitoring-console", fmt.Sprintf("splunk-%s-monitoring-console-0", lm.Name), lm.Namespace, lm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-monitoring-console", lm.Name), lm.Namespace)
	// mointoring console statefulset is created here
	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}

	err = client.Get(ctx, namespacedName, &mc)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	if mc.Status.Phase != enterpriseApi.PhaseReady {
		t.Errorf("mc is not in ready state")
	}

	// ------- Step2 starts here -----
	// Update
	// standalone
	err = client.Get(ctx, namespacedName, &stdln)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	stdln.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &stdln)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	// cluster manager
	err = client.Get(ctx, namespacedName, &cm)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	cm.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &cm)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	// license manager
	err = client.Get(ctx, namespacedName, &lm)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}
	lm.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &lm)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	// monitoring console
	err = client.Get(ctx, namespacedName, &mc)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}
	mc.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &mc)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	// indexer cluster console
	err = client.Get(ctx, namespacedName, &idx)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}
	idx.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &idx)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	// searchhead cluster console
	err = client.Get(ctx, namespacedName, &shc)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}
	shc.Spec.Image = "splunk/splunk:latest"
	err = client.Update(ctx, &shc)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	lm.Status.TelAppInstalled = true
	_, err = reconcile.ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	lm.Status.TelAppInstalled = true
	_, err = reconcile.ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}
	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}
	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil {
		t.Errorf("ApplyIndexerClusterManager after update should not have returned error; err=%v", err)
	}
	newImage := "splunk/splunk:latest"
	// create pods for license manager
	spltest.CreatePods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, newImage)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true

	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "cluster-manager", fmt.Sprintf("splunk-%s-cluster-manager-0", cm.Name), cm.Namespace, cm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-cluster-manager", cm.Name), cm.Namespace)
	cm.Status.TelAppInstalled = true

	// create pods for indexer cluster
	spltest.CreatePods(t, ctx, client, "indexer", fmt.Sprintf("splunk-%s-indexer-0", idx.Name), idx.Namespace, newImage)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-indexer", idx.Name), idx.Namespace)

	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "monitoring-console", fmt.Sprintf("splunk-%s-monitoring-console-0", lm.Name), lm.Namespace, newImage)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-monitoring-console", lm.Name), lm.Namespace)

	// create pods for cluster manager
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-0", shc.Name), shc.Namespace, newImage)
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-1", shc.Name), shc.Namespace, newImage)
	spltest.CreatePods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-2", shc.Name), shc.Namespace, newImage)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 3, fmt.Sprintf("splunk-%s-search-head", shc.Name), shc.Namespace)
	spltest.CreatePods(t, ctx, client, "deployer", fmt.Sprintf("splunk-%s-deployer-0", shc.Name), shc.Namespace, newImage)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-deployer", shc.Name), shc.Namespace)
	shc.Status.TelAppInstalled = true

	lm.Status.TelAppInstalled = true
	_, err = reconcile.ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = enterprise.ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}

	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	_, err = enterprise.ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = searchheadcluster.ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}

	_, err = indexercluster.ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil {
		t.Errorf("ApplyIndexerClusterManager after update should not have returned error; err=%v", err)
	}

}

// TestUpgradePathValidation_LicenseManagerGate verifies that the LicenseManager
// gate in UpgradePathValidation distinguishes a transient not-Ready phase (soft
// wait, no error) from a genuine image mismatch (hard error), instead of
// conflating both into a single fatal error.
func TestUpgradePathValidation_LicenseManagerGate(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{})
	client := builder.Build()
	ctx := context.TODO()

	lm := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-lm-gate", Namespace: "test"},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:old"},
			},
		},
	}
	if err := client.Create(ctx, &lm); err != nil {
		t.Fatalf("Failed to create LicenseManager: %v", err)
	}

	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm-gate", Namespace: "test"},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec:              enterpriseApi.Spec{Image: "splunk/splunk:old"},
				LicenseManagerRef: corev1.ObjectReference{Name: "test-lm-gate"},
			},
		},
	}
	cm.Kind = "ClusterManager"
	if err := client.Create(ctx, &cm); err != nil {
		t.Fatalf("Failed to create ClusterManager: %v", err)
	}

	lmSS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-lm-gate-license-manager", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:old"}}},
			},
		},
	}
	if err := client.Create(ctx, lmSS); err != nil {
		t.Fatalf("Failed to create LicenseManager StatefulSet: %v", err)
	}

	// LicenseManager image matches, but it's not yet Ready (e.g. mid pod recycle):
	// expect a soft wait, not an error.
	lm.Status.Phase = enterpriseApi.PhaseUpdating
	if err := client.Status().Update(ctx, &lm); err != nil {
		t.Fatalf("Failed to update LicenseManager status: %v", err)
	}

	continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, &cm, cm.Spec.CommonSplunkSpec, nil)
	if err != nil {
		t.Errorf("Expected no error when LicenseManager is transiently not Ready, got: %v", err)
	}
	if continueReconcile {
		t.Errorf("Expected continueReconcile to be false while LicenseManager is not Ready")
	}

	// LicenseManager is Ready, but its current image differs from the CR spec
	// image: expect a hard error.
	lm.Status.Phase = enterpriseApi.PhaseReady
	if err := client.Status().Update(ctx, &lm); err != nil {
		t.Fatalf("Failed to update LicenseManager status: %v", err)
	}
	lmSS.Spec.Template.Spec.Containers[0].Image = "splunk/splunk:new"
	if err := client.Update(ctx, lmSS); err != nil {
		t.Fatalf("Failed to update LicenseManager StatefulSet image: %v", err)
	}

	continueReconcile, err = upgrade.UpgradePathValidation(ctx, client, &cm, cm.Spec.CommonSplunkSpec, nil)
	if err == nil {
		t.Errorf("Expected an error when LicenseManager image differs from CR image")
	}
	if continueReconcile {
		t.Errorf("Expected continueReconcile to be false when LicenseManager image mismatches CR image")
	}
}

func TestUpgradeBlockedVersionMismatchEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{})

	client := builder.Build()
	ctx := context.TODO()

	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher, err := k8sops.NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.IndexerCluster{})
	if err != nil {
		t.Fatalf("failed to create event publisher: %v", err)
	}

	// Create ClusterManager with old image, phase Ready
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test"},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:old"},
			},
		},
	}
	cm.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("ClusterManager"))
	if err := client.Create(ctx, &cm); err != nil {
		t.Fatalf("Failed to create ClusterManager: %v", err)
	}
	cm.Status.Phase = enterpriseApi.PhaseReady
	if err := client.Status().Update(ctx, &cm); err != nil {
		t.Fatalf("Failed to update ClusterManager status: %v", err)
	}

	// Create CM statefulset with old image
	cmSS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-cm-cluster-manager", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:old"}}},
			},
		},
	}
	if err := client.Create(ctx, cmSS); err != nil {
		t.Fatalf("Failed to create CM StatefulSet: %v", err)
	}

	// IndexerCluster CR with NEW image (mismatch with CM)
	idx := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-idx", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec:              enterpriseApi.Spec{Image: "splunk/splunk:new"},
				ClusterManagerRef: corev1.ObjectReference{Name: "test-cm"},
			},
		},
	}
	idx.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))

	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, &idx, idx.Spec.CommonSplunkSpec, nil)

	if continueReconcile {
		t.Errorf("Expected continueReconcile to be false when CM image mismatches IDX image")
	}
	if err == nil {
		t.Errorf("Expected error when CM image mismatches IDX image")
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "UpgradeBlockedVersionMismatch" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for UpgradeBlockedVersionMismatch, got %s", event.eventType)
			}
			expectedMessage := "Upgrade blocked: ClusterManager version splunk/splunk:old != IndexerCluster version splunk/splunk:new. Upgrade ClusterManager first."
			if event.message != expectedMessage {
				t.Errorf("Expected event message %q, got: %q", expectedMessage, event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected UpgradeBlockedVersionMismatch event to be published")
	}
}

func newFakeClientBuilder(scheme *pkgruntime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				if gvk, err := apiutil.GVKForObject(obj, scheme); err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}

type mockEvent struct {
	eventType string
	reason    string
	message   string
}

type mockEventRecorder struct {
	events []mockEvent
}

func (m *mockEventRecorder) Event(_ pkgruntime.Object, eventType, reason, message string) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: message})
}

func (m *mockEventRecorder) Eventf(_ pkgruntime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

func (m *mockEventRecorder) AnnotatedEventf(_ pkgruntime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}
