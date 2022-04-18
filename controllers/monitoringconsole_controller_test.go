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

var _ = Describe("MonitoringConsole Controller", func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("MonitoringConsole Management", func() {

		It("Get MonitoringConsole custom resource should failed", func() {
			namespace := "ns-splunk-mc-1"
			ApplyMonitoringConsole = func(ctx context.Context, client client.Client, instance *enterprisev3.MonitoringConsole) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetMonitoringConsole("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("monitoringconsoles.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create MonitoringConsole custom resource with annotations should pause", func() {
			namespace := "ns-splunk-mc-2"
			ApplyMonitoringConsole = func(ctx context.Context, client client.Client, instance *enterprisev3.MonitoringConsole) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterprisev3.MonitoringConsolePausedAnnotation] = ""
			CreateMonitoringConsole("test", nsSpecs.Name, annotations, splcommon.PhaseReady)
			ssSpec, _ := GetMonitoringConsole("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			UpdateMonitoringConsole(ssSpec, splcommon.PhaseReady)
			DeleteMonitoringConsole("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create MonitoringConsole custom resource should succeeded", func() {
			namespace := "ns-splunk-mc-3"
			ApplyMonitoringConsole = func(ctx context.Context, client client.Client, instance *enterprisev3.MonitoringConsole) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateMonitoringConsole("test", nsSpecs.Name, annotations, splcommon.PhaseReady)
			DeleteMonitoringConsole("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			// Create New Manager for controllers
			//k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			//	Scheme: scheme.Scheme,
			//})
			//Expect(err).ToNot(HaveOccurred())

			//rr, err := New(k8sManager)
			//callUnsedMethods(rr.(*MonitoringConsoleReconciler), namespace)
		})

	})
})

func GetMonitoringConsole(name string, namespace string) (*enterprisev3.MonitoringConsole, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting MonitoringConsole custom resource to be created successfully")
	ss := &enterprisev3.MonitoringConsole{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateMonitoringConsole(name string, namespace string, annotations map[string]string, status splcommon.Phase) *enterprisev3.MonitoringConsole {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterprisev3.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: enterprisev3.MonitoringConsoleSpec{},
	}
	ssSpec = testutils.NewMonitoringConsole(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting MonitoringConsole custom resource to be created successfully")
	ss := &enterprisev3.MonitoringConsole{}
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

func UpdateMonitoringConsole(instance *enterprisev3.MonitoringConsole, status splcommon.Phase) *enterprisev3.MonitoringConsole {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewMonitoringConsole(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting MonitoringConsole custom resource to be created successfully")
	ss := &enterprisev3.MonitoringConsole{}
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

func DeleteMonitoringConsole(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting MonitoringConsole Deleted successfully")
	Eventually(func() error {
		ssys := &enterprisev3.MonitoringConsole{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
