package enterprise

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

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
	_, err = ApplyStandalone(ctx, client, &stdln)
	if err != nil {
		t.Errorf("ApplyStandalone should not have returned error; err=%v", err)
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

	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManagershould not have returned error; err=%v", err)
	}

	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}

	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	// license manager statefulset is not created
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create license manager statefulset
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error; err=%v", err)
	}

	// create pods for license manager
	createPods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true
	// create license manager statefulset
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	// cluster manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	// cluster manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManagershould not have returned error; err=%v", err)
	}

	_, err = ApplyMonitoringConsole(ctx, client, &mc)
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

	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	// lm statefulset should have been created by now, this should pass
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create pods for cluster manager
	createPods(t, ctx, client, "cluster-manager", fmt.Sprintf("splunk-%s-cluster-manager-0", cm.Name), cm.Namespace, cm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-cluster-manager", cm.Name), cm.Namespace)
	cm.Status.TelAppInstalled = true
	// cluster manager is found  and creat
	_, err = ApplyClusterManager(ctx, client, &cm, nil)
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
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	// monitoring console statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	// mock the verify RF peer function
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	// monitoring console statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	// Monitoring console is ready now, now this should crete statefulset but statefulset is not in ready phase
	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should not have returned error; err=%v", err)
	}

	// create pods for cluster manager
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-0", shc.Name), shc.Namespace, shc.Spec.Image)
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-1", shc.Name), shc.Namespace, shc.Spec.Image)
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-2", shc.Name), shc.Namespace, shc.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 3, fmt.Sprintf("splunk-%s-search-head", shc.Name), shc.Namespace)
	createPods(t, ctx, client, "deployer", fmt.Sprintf("splunk-%s-deployer-0", shc.Name), shc.Namespace, shc.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-deployer", shc.Name), shc.Namespace)

	// used in mocking this function
	GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
		shcm := &splclient.SearchHeadClusterMemberInfo{
			Status: "Up",
		}
		return shcm, nil
	}

	// used in mocking this function
	GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
		shci := &splclient.SearchHeadCaptainInfo{
			ServiceReady: true,
			Initialized:  true,
		}
		return shci, nil
	}
	// Now SearchheadCluster should move to READY state
	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
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

	// mock the verify RF peer function
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	// mock the call
	GetClusterInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager, mockCall bool) (*splclient.ClusterInfo, error) {
		cinfo := &splclient.ClusterInfo{
			MultiSite: "false",
		}
		return cinfo, nil
	}
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		response := map[string]splclient.ClusterManagerPeerInfo{
			"splunk-test-indexer-0": {
				ID:             "site-1",
				Status:         "Up",
				ActiveBundleID: "1",
				BucketCount:    10,
				Searchable:     true,
			},
		}
		return response, err
	}
	GetClusterManagerInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error) {
		response := &splclient.ClusterManagerInfo{
			Initialized:   true,
			IndexingReady: true,
			ServiceReady:  true,
		}
		return response, err
	}

	// search head cluster is ready, this should create statefulset but they are not ready
	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	// create pods for indexer cluster
	createPods(t, ctx, client, "indexer", fmt.Sprintf("splunk-%s-indexer-0", idx.Name), idx.Namespace, idx.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-indexer", idx.Name), idx.Namespace)

	// search head cluster is not ready, so wait for search head cluster
	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
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

	GetCMMultisiteEnvVarsCall = func(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := getClusterManagerExtraEnv(cr, &cr.Spec.CommonSplunkSpec)
		return extraEnv, err
	}

	// mointoring console statefulset is created here
	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}
	// create pods for cluster manager
	createPods(t, ctx, client, "monitoring-console", fmt.Sprintf("splunk-%s-monitoring-console-0", lm.Name), lm.Namespace, lm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-monitoring-console", lm.Name), lm.Namespace)
	// mointoring console statefulset is created here
	_, err = ApplyMonitoringConsole(ctx, client, &mc)
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
	_, err = ApplyStandalone(ctx, client, &stdln)
	if err != nil {
		t.Errorf("ApplyStandalone should not have returned error; err=%v", err)
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
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	lm.Status.TelAppInstalled = true
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}
	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}
	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil {
		t.Errorf("ApplyIndexerClusterManager after update should not have returned error; err=%v", err)
	}
	newImage := "splunk/splunk:latest"
	// create pods for license manager
	createPods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, newImage)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true

	// create pods for cluster manager
	createPods(t, ctx, client, "cluster-manager", fmt.Sprintf("splunk-%s-cluster-manager-0", cm.Name), cm.Namespace, cm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-cluster-manager", cm.Name), cm.Namespace)
	cm.Status.TelAppInstalled = true

	// create pods for indexer cluster
	createPods(t, ctx, client, "indexer", fmt.Sprintf("splunk-%s-indexer-0", idx.Name), idx.Namespace, newImage)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-indexer", idx.Name), idx.Namespace)

	// create pods for cluster manager
	createPods(t, ctx, client, "monitoring-console", fmt.Sprintf("splunk-%s-monitoring-console-0", lm.Name), lm.Namespace, newImage)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-monitoring-console", lm.Name), lm.Namespace)

	// create pods for cluster manager
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-0", shc.Name), shc.Namespace, newImage)
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-1", shc.Name), shc.Namespace, newImage)
	createPods(t, ctx, client, "search-head", fmt.Sprintf("splunk-%s-search-head-2", shc.Name), shc.Namespace, newImage)
	updateStatefulSetsInTest(t, ctx, client, 3, fmt.Sprintf("splunk-%s-search-head", shc.Name), shc.Namespace)
	createPods(t, ctx, client, "deployer", fmt.Sprintf("splunk-%s-deployer-0", shc.Name), shc.Namespace, newImage)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-deployer", shc.Name), shc.Namespace)
	shc.Status.TelAppInstalled = true

	lm.Status.TelAppInstalled = true
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}

	cm.Status.TelAppInstalled = true
	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}

	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}

	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}

	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil {
		t.Errorf("ApplyIndexerClusterManager after update should not have returned error; err=%v", err)
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
	eventPublisher := &K8EventPublisher{recorder: recorder}

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

	mgr := &indexerClusterPodManager{}
	continueReconcile, err := UpgradePathValidation(ctx, client, &idx, idx.Spec.CommonSplunkSpec, mgr)

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
			expectedMessage := "ClusterManager dependency test/test-cm requests image splunk/splunk:old but the dependent resource requests splunk/splunk:new"
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

func createPods(t *testing.T, ctx context.Context, client common.ControllerClient, crtype, name, namespace, image string) {
	stpod := &corev1.Pod{}
	namespacesName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := client.Get(ctx, namespacesName, stpod)
	if err != nil && k8serrors.IsNotFound(err) {
		// create pod
		stpod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "splunk-operator",
					"app.kubernetes.io/component":  crtype,
					"app.kubernetes.io/name":       crtype,
					"app.kubernetes.io/part-of":    fmt.Sprintf("splunk-test-%s", crtype),
					"app.kubernetes.io/instance":   fmt.Sprintf("splunk-test-%s", crtype),
				},
				Annotations: map[string]string{
					"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
					"traffic.sidecar.istio.io/includeInboundPorts":  "8000",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "splunk",
						Image: image,
						Env: []corev1.EnvVar{
							{
								Name:  "test",
								Value: "test",
							},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "http-splunkweb",
								HostPort:      0,
								ContainerPort: 8000,
								Protocol:      "TCP",
								HostIP:        "",
							},
							{
								Name:          "https-splunkd",
								HostPort:      0,
								ContainerPort: 8089,
								Protocol:      "TCP",
								HostIP:        "",
							},
						},
					},
				},
			},
		}
		// simulate create stateful set
		err := client.Create(ctx, stpod)
		if err != nil {
			t.Errorf("Unexpected create pod failed %v", err)
			debug.PrintStack()
		}
	} else if err != nil {
		t.Errorf("Unexpected erro while get pod  %v", err)
		debug.PrintStack()
	}
	if stpod.Spec.Containers[0].Image != image {
		stpod.Spec.Containers[0].Image = image
		err := client.Update(ctx, stpod)
		if err != nil {
			t.Errorf("Unexpected create pod failed %v", err)
			debug.PrintStack()
		}
	}

	// update statefulset
	stpod.Status.Phase = corev1.PodRunning
	stpod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: image,
			Name:  "splunk",
			Ready: true,
		},
	}
	err = client.Status().Update(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}
}

func updateStatefulSetsInTest(t *testing.T, ctx context.Context, client common.ControllerClient, replicas int32, name, namespace string) {
	stNamespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	statefulset := &appsv1.StatefulSet{}
	err := client.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}
	// update statefulset
	statefulset.Status.ReadyReplicas = replicas
	statefulset.Status.Replicas = replicas
	statefulset.Status.CurrentReplicas = replicas
	statefulset.Status.AvailableReplicas = replicas
	err = client.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}
}

func TestUpgradePathValidationClassifiesLicenseManagerDependency(t *testing.T) {
	ctx := context.Background()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	newSHC := func() *enterpriseApi.SearchHeadCluster {
		return &enterpriseApi.SearchHeadCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterpriseApi.GroupVersion.String(),
				Kind:       "SearchHeadCluster",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "search", Namespace: "test"},
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec:              enterpriseApi.Spec{Image: "splunk/enterprise:target"},
					LicenseManagerRef: corev1.ObjectReference{Name: "license"},
				},
			},
		}
	}

	newLicenseManager := func(phase enterpriseApi.Phase, desiredImage string) *enterpriseApi.LicenseManager {
		return &enterpriseApi.LicenseManager{
			ObjectMeta: metav1.ObjectMeta{Name: "license", Namespace: "test"},
			Spec: enterpriseApi.LicenseManagerSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: desiredImage},
				},
			},
			Status: enterpriseApi.LicenseManagerStatus{Phase: phase},
		}
	}

	newLicenseManagerStatefulSet := func(image string) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetSplunkStatefulsetName(SplunkLicenseManager, "license"),
				Namespace: "test",
			},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "splunk", Image: image}},
					},
				},
			},
		}
	}

	t.Run("missing referenced object is a retryable dependency wait", func(t *testing.T) {
		client := newFakeClientBuilder(scheme).Build()
		shc := newSHC()
		continueReconcile, err := UpgradePathValidation(ctx, client, shc, shc.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok {
			t.Fatalf("error = %T %v, want DependencyNotReadyError", err, err)
		}
		if wait.Kind != "LicenseManager" || wait.Name != "license" || wait.Namespace != "test" {
			t.Fatalf("dependency = %#v, want LicenseManager test/license", wait)
		}
	})

	t.Run("existing Pending object is a retryable dependency wait", func(t *testing.T) {
		lm := newLicenseManager(enterpriseApi.PhasePending, "splunk/enterprise:target")
		client := newFakeClientBuilder(scheme).WithObjects(lm).Build()
		shc := newSHC()
		continueReconcile, err := UpgradePathValidation(ctx, client, shc, shc.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok {
			t.Fatalf("error = %T %v, want DependencyNotReadyError", err, err)
		}
		if wait.Phase != enterpriseApi.PhasePending {
			t.Fatalf("observed phase = %q, want Pending", wait.Phase)
		}
	})

	t.Run("desired target with old current image is a retryable convergence wait", func(t *testing.T) {
		lm := newLicenseManager(enterpriseApi.PhaseReady, "splunk/enterprise:target")
		sts := newLicenseManagerStatefulSet("splunk/enterprise:old")
		client := newFakeClientBuilder(scheme).WithObjects(lm, sts).Build()
		shc := newSHC()
		continueReconcile, err := UpgradePathValidation(ctx, client, shc, shc.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok {
			t.Fatalf("error = %T %v, want DependencyNotReadyError", err, err)
		}
		if wait.ObservedImage != "splunk/enterprise:old" || wait.DesiredImage != "splunk/enterprise:target" {
			t.Fatalf("images = observed %q desired %q", wait.ObservedImage, wait.DesiredImage)
		}
	})

	t.Run("different desired images are a terminal configuration mismatch", func(t *testing.T) {
		lm := newLicenseManager(enterpriseApi.PhaseReady, "splunk/enterprise:old")
		sts := newLicenseManagerStatefulSet("splunk/enterprise:old")
		client := newFakeClientBuilder(scheme).WithObjects(lm, sts).Build()
		shc := newSHC()
		continueReconcile, err := UpgradePathValidation(ctx, client, shc, shc.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		if _, ok := AsDependencyNotReady(err); ok {
			t.Fatalf("mismatch returned retryable dependency wait: %v", err)
		}
		reason, ok := splcommon.TerminalReason(err)
		if !ok || reason != EventReasonUpgradeBlockedVersionMismatch {
			t.Fatalf("terminal reason = %q, %t; want %q", reason, ok, EventReasonUpgradeBlockedVersionMismatch)
		}
	})
}

func TestDependencyWaitPhaseAndConditions(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterpriseApi.GroupVersion.String(),
			Kind:       "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "search",
			Namespace:  "test",
			Generation: 9,
		},
	}
	recorder := record.NewFakeRecorder(1)
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: cr},
	)
	waitErr := newDependencyNotReady(
		"LicenseManager",
		"test",
		"license",
		enterpriseApi.PhasePending,
		"",
		"splunk/enterprise:target",
		"",
	)

	status, waiting := dependencyWaitPhaseAndConditions(ctx, cr, nil, false, waitErr)
	if !waiting {
		t.Fatal("waiting = false, want true")
	}
	if status.Phase != enterpriseApi.PhasePending {
		t.Fatalf("phase = %q, want Pending", status.Phase)
	}
	ready := meta.FindStatusCondition(status.Conditions, string(enterpriseApi.ConditionReady))
	progressing := meta.FindStatusCondition(status.Conditions, string(enterpriseApi.ConditionProgressing))
	if ready == nil || ready.Reason != string(enterpriseApi.ReasonDependencyNotReady) {
		t.Fatalf("Ready condition = %#v, want DependencyNotReady", ready)
	}
	if progressing == nil || progressing.Reason != string(enterpriseApi.ReasonDependencyNotReady) {
		t.Fatalf("Progressing condition = %#v, want DependencyNotReady", progressing)
	}
	event := <-recorder.Events
	if !strings.Contains(event, "Normal DependencyNotReady") || !strings.Contains(event, "LicenseManager dependency test/license") {
		t.Fatalf("event = %q, want aggregatable DependencyNotReady detail", event)
	}
}

func TestUpgradePathValidationClassifiesClusterManagerDependency(t *testing.T) {
	ctx := context.Background()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	indexer := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterpriseApi.GroupVersion.String(),
			Kind:       "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "indexers", Namespace: "workload"},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/enterprise:target"},
				ClusterManagerRef: corev1.ObjectReference{
					Name:      "manager",
					Namespace: "dependencies",
				},
			},
		},
	}

	t.Run("missing cross-namespace reference waits on the referenced namespace", func(t *testing.T) {
		client := newFakeClientBuilder(scheme).Build()
		continueReconcile, err := UpgradePathValidation(ctx, client, indexer, indexer.Spec.CommonSplunkSpec, &indexerClusterPodManager{})
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok {
			t.Fatalf("error = %T %v, want DependencyNotReadyError", err, err)
		}
		if wait.Kind != "ClusterManager" || wait.Namespace != "dependencies" || wait.Name != "manager" {
			t.Fatalf("dependency = %#v, want ClusterManager dependencies/manager", wait)
		}
	})

	t.Run("Pending cross-namespace reference reports observed phase", func(t *testing.T) {
		manager := &enterpriseApi.ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "dependencies"},
			Spec: enterpriseApi.ClusterManagerSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/enterprise:target"},
				},
			},
			Status: enterpriseApi.ClusterManagerStatus{Phase: enterpriseApi.PhasePending},
		}
		client := newFakeClientBuilder(scheme).WithObjects(manager).Build()
		continueReconcile, err := UpgradePathValidation(ctx, client, indexer, indexer.Spec.CommonSplunkSpec, &indexerClusterPodManager{})
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok || wait.Phase != enterpriseApi.PhasePending {
			t.Fatalf("dependency wait = %#v, %t, want Pending", wait, ok)
		}
	})
}

func TestUpgradePathValidationMonitoringConsoleReverseDependencies(t *testing.T) {
	ctx := context.Background()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	newMonitoringConsole := func() *enterpriseApi.MonitoringConsole {
		return &enterpriseApi.MonitoringConsole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterpriseApi.GroupVersion.String(),
				Kind:       "MonitoringConsole",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "monitor", Namespace: "test"},
			Spec: enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/enterprise:target"},
				},
			},
		}
	}

	t.Run("IndexerCluster reference is not skipped when its name differs", func(t *testing.T) {
		indexer := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "indexers", Namespace: "test"},
			Spec: enterpriseApi.IndexerClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					MonitoringConsoleRef: corev1.ObjectReference{Name: "monitor"},
				},
			},
			Status: enterpriseApi.IndexerClusterStatus{Phase: enterpriseApi.PhasePending},
		}
		client := newFakeClientBuilder(scheme).WithObjects(indexer).Build()
		monitor := newMonitoringConsole()
		continueReconcile, err := UpgradePathValidation(ctx, client, monitor, monitor.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok || wait.Kind != "IndexerCluster" || wait.Name != "indexers" {
			t.Fatalf("dependency wait = %#v, %t, want IndexerCluster test/indexers", wait, ok)
		}
	})

	t.Run("Standalone references are listed as Standalones", func(t *testing.T) {
		standalone := &enterpriseApi.Standalone{
			ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "test"},
			Spec: enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					MonitoringConsoleRef: corev1.ObjectReference{Name: "monitor"},
				},
			},
			Status: enterpriseApi.StandaloneStatus{Phase: enterpriseApi.PhasePending},
		}
		client := newFakeClientBuilder(scheme).WithObjects(standalone).Build()
		monitor := newMonitoringConsole()
		continueReconcile, err := UpgradePathValidation(ctx, client, monitor, monitor.Spec.CommonSplunkSpec, nil)
		if continueReconcile {
			t.Fatal("continueReconcile = true, want false")
		}
		wait, ok := AsDependencyNotReady(err)
		if !ok || wait.Kind != "Standalone" || wait.Name != "standalone" {
			t.Fatalf("dependency wait = %#v, %t, want Standalone test/standalone", wait, ok)
		}
	})
}
