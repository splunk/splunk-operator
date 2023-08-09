package enterprise

import (
	"context"
	"testing"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
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

	_, err = ApplyIndexerCluster(ctx, client, &idx)
	// license manager statefulset is not created so if its NotFound error we are good
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Errorf("applyIndexerCluster should not have returned error; err=%v", err)
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

	_, err = ApplyClusterManager(ctx, client, &cm)
	// lm statefulset should have been created by now, this should pass
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}





	// Update
	// standalone
	namespacedName := types.NamespacedName{
		Name:      "test",
		Namespace: "test",
	}
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

	_, err = ApplyClusterManager(ctx, client, &cm)
	if err != nil {
		t.Errorf("applyClusterManager after update should not have returned error; err=%v", err)
	}
	_, err = ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager after update should not have returned error; err=%v", err)
	}
	_, err = ApplyMonitoringConsole(ctx, client, &mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole after update should not have returned error; err=%v", err)
	}
	_, err = ApplyIndexerCluster(ctx, client, &idx)
	if err != nil {
		t.Errorf("applyIndexerCluster after update should not have returned error; err=%v", err)
	}
	_, err = ApplySearchHeadCluster(ctx, client, &shc)
	if err != nil {
		t.Errorf("applySearchHeadCluster after update should not have returned error; err=%v", err)
	}
}
