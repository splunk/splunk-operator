package controllers

import (
	"context"
	"fmt"

	//"reflect"
	"time"

	enterprisev3 "github.com/splunk/splunk-operator/api/v3"
	"github.com/splunk/splunk-operator/controllers/testutils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	//ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	//"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("ClusterManager Controller", func() {

	var (
		namespace = "ns-splunk-cm"
	)

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("ClusterManager Management", func() {

		It("Create ClusterManager custom resource should succeeded", func() {
			ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterprisev3.ClusterManager) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}

			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			CreateClusterManager("test", nsSpecs.Name, splcommon.PhaseReady)
			DeleteClusterManager("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
		It("Cover Unused methods", func() {
			// Create New Manager for controllers
			//k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			//	Scheme: scheme.Scheme,
			//})
			//Expect(err).ToNot(HaveOccurred())

			//rr, err := New(k8sManager)
			//callUnsedMethods(rr.(*ClusterManagerReconciler), namespace)
		})

	})
})

func CreateClusterManager(name string, namespace string, status splcommon.Phase) *enterprisev3.ClusterManager {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterprisev3.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: enterprisev3.ClusterManagerSpec{},
	}
	ssSpec = testutils.NewClusterManager(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting ClusterManager custom resource to be created successfully")
	ss := &enterprisev3.ClusterManager{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func DeleteClusterManager(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting ClusterManager Deleted successfully")
	Eventually(func() error {
		ssys := &enterprisev3.ClusterManager{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
