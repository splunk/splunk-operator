package enterprise

import (
	"context"
	"fmt"
	"runtime/debug"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)



func TestUpgradePathValidation(t *testing.T) {

	builder := fake.NewClientBuilder()
	client := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))

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

	_, err = ApplyClusterManager(ctx, client, &cm)
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

	_, err = ApplyClusterManager(ctx, client, &cm)
	// lm statefulset should have been created by now, this should pass
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create pods for cluster manager
	createPods(t, ctx, client, "cluster-manager", fmt.Sprintf("splunk-%s-cluster-manager-0", cm.Name), cm.Namespace, cm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-cluster-manager", cm.Name), cm.Namespace)
	cm.Status.TelAppInstalled = true
	// cluster manager is found  and creat
	_, err = ApplyClusterManager(ctx, client, &cm)
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

	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	// monitoring console statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManagershould not have returned error; err=%v", err)
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

	// mock the verify RF peer funciton
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}
	// search head cluster is ready, this should create statefulset but they are not ready
	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("ApplyIndexerClusterManager should not have returned error; err=%v", err)
	}

	// create pods for indexer cluster
	createPods(t , ctx, client, "indexer", fmt.Sprintf("splunk-%s-indexer-0",idx.Name), idx.Namespace, idx.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-indexer",idx.Name), idx.Namespace)

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

	// ------- Step2 starts here -----
	// Update
	// standalone
	err = client.Get(ctx, namespacedName, &lm)
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

	cm.Status.TelAppInstalled = true
	_, err = ApplyClusterManager(ctx, client, &cm)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}
	lm.Status.TelAppInstalled = true
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}
	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}
	_, err = ApplyIndexerClusterManager(ctx, client, &idx)
	if err != nil {
		t.Errorf("ApplyIndexerClusterManager after update should not have returned error; err=%v", err)
	}
	shc.Status.TelAppInstalled = true
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}
}

func createPods(t *testing.T, ctx context.Context, client common.ControllerClient, crtype, name, namespace, image string) {
	// create pod
	stpod := &corev1.Pod{
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
		t.Errorf("Unexpected update statefulset  %v", err)
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
	err = client.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}
}
