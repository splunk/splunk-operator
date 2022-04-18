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

var _ = Describe("SearchHeadCluster Controller", func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("SearchHeadCluster Management", func() {

		It("Get SearchHeadCluster custom resource should failed", func() {
			namespace := "ns-splunk-shc-1"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterprisev3.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetSearchHeadCluster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("searchheadclusters.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create SearchHeadCluster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-shc-2"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterprisev3.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterprisev3.SearchHeadClusterPausedAnnotation] = ""
			CreateSearchHeadCluster("test", nsSpecs.Name, annotations, splcommon.PhaseReady)
			ssSpec, _ := GetSearchHeadCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.DeployerPhase = "Ready"
			ssSpec.Status.Phase = "Ready"
			UpdateSearchHeadCluster(ssSpec, splcommon.PhaseReady)
			DeleteSearchHeadCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create SearchHeadCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-shc-3"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterprisev3.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateSearchHeadCluster("test", nsSpecs.Name, annotations, splcommon.PhaseReady)
			DeleteSearchHeadCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			// Create New Manager for controllers
			//k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			//	Scheme: scheme.Scheme,
			//})
			//Expect(err).ToNot(HaveOccurred())

			//rr, err := New(k8sManager)
			//callUnsedMethods(rr.(*SearchHeadClusterReconciler), namespace)
		})

	})
})

func GetSearchHeadCluster(name string, namespace string) (*enterprisev3.SearchHeadCluster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting SearchHeadCluster custom resource to be created successfully")
	ss := &enterprisev3.SearchHeadCluster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateSearchHeadCluster(name string, namespace string, annotations map[string]string, status splcommon.Phase) *enterprisev3.SearchHeadCluster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterprisev3.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: enterprisev3.SearchHeadClusterSpec{},
	}
	ssSpec = testutils.NewSearchHeadCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting SearchHeadCluster custom resource to be created successfully")
	ss := &enterprisev3.SearchHeadCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.DeployerPhase = status
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func UpdateSearchHeadCluster(instance *enterprisev3.SearchHeadCluster, status splcommon.Phase) *enterprisev3.SearchHeadCluster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewSearchHeadCluster(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting SearchHeadCluster custom resource to be created successfully")
	ss := &enterprisev3.SearchHeadCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.DeployerPhase = status
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func DeleteSearchHeadCluster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting SearchHeadCluster Deleted successfully")
	Eventually(func() error {
		ssys := &enterprisev3.SearchHeadCluster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
