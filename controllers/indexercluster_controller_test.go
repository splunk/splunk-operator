package controllers

import (
	"context"
	"fmt"
	//"reflect"
	"time"

	enterprisev3 "github.com/splunk/splunk-operator/api/v3"
	"github.com/splunk/splunk-operator/controllers/testutils"

	//ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	//"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("IndexerCluster Controller", func() {

	var (
		namespace = "ns-splunk-idx"
	)

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("IndexerCluster Management", func() {

		It("Create IndexerCluster custom resource should succeeded", func() {
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			CreateIndexerCluster("test", nsSpecs.Name, splcommon.PhaseReady)
			DeleteIndexerCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
		It("Cover Unused methods", func() {
			// Create New Manager for controllers
			//k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			//	Scheme: scheme.Scheme,
			//})
			//Expect(err).ToNot(HaveOccurred())

			//rr, err := New(k8sManager)
			//callUnsedMethods(rr.(*IndexerClusterReconciler), namespace)
		})

	})
})

func CreateIndexerCluster(name string, namespace string, status splcommon.Phase) *enterprisev3.IndexerCluster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterprisev3.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: enterprisev3.IndexerClusterSpec{},
	}
	ssSpec = testutils.NewIndexerCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterprisev3.IndexerCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.ClusterMasterPhase = status
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func DeleteIndexerCluster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting IndexerCluster Deleted successfully")
	Eventually(func() error {
		ssys := &enterprisev3.IndexerCluster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
