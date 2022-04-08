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

const timeout = time.Second * 120
const interval = time.Second * 2

var _ = Describe("Standalone Controller", func() {

	var (
		namespace = "ns-splunk-st"
	)

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("Standalone Management", func() {

		It("Create Standalone custom resource should succeeded", func() {
			ApplyStandalone = func(ctx context.Context, client client.Client, instance *enterprisev3.Standalone) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			CreateStandlaone("test", nsSpecs.Name, splcommon.PhaseReady)
			DeleteStandalone("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
		It("Cover Unused methods", func() {
			// Create New Manager for controllers
			//k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			//	Scheme: scheme.Scheme,
			//})
			//Expect(err).ToNot(HaveOccurred())

			//rr, err := New(k8sManager)
			//callUnsedMethods(rr.(*StandaloneReconciler), namespace)
		})

	})
})

func callUnsedMethods(rr *StandaloneReconciler, namespace string) {
	key := types.NamespacedName{
		Name:      "secret5",
		Namespace: namespace,
	}

	secret := &corev1.Secret{}
	_ = k8sClient.Get(context.TODO(), key, secret)
}

func CreateStandlaone(name string, namespace string, status splcommon.Phase) *enterprisev3.Standalone {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterprisev3.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: enterprisev3.StandaloneSpec{},
	}
	ssSpec = testutils.NewStandalone(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting Standalone custom resource to be created successfully")
	ss := &enterprisev3.Standalone{}
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

func DeleteStandalone(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting Standalone Deleted successfully")
	Eventually(func() error {
		ssys := &enterprisev3.Standalone{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
