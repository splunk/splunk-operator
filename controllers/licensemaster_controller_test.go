package controllers

import (
	"context"
	"fmt"

	//"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	"github.com/splunk/splunk-operator/controllers/testutils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	//ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	//"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("LicenseMaster Controller", func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("LicenseMaster Management", func() {

		It("Get LicenseMaster custom resource should failed", func() {
			namespace := "ns-splunk-lm-1"
			ApplyLicenseManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.LicenseMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetLicenseMaster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("licensemasters.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create LicenseMaster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-lm-2"
			ApplyLicenseManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.LicenseMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApi.LicenseManagerPausedAnnotation] = ""
			CreateLicenseMaster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetLicenseMaster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			UpdateLicenseMaster(ssSpec, enterpriseApi.PhaseReady)
			DeleteLicenseMaster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create LicenseMaster custom resource should succeeded", func() {
			namespace := "ns-splunk-lm-3"
			ApplyLicenseManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.LicenseMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateLicenseMaster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteLicenseMaster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-lm-4"
			ApplyLicenseManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.LicenseMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := LicenseMasterReconciler{
				Client: c,
				Scheme: scheme.Scheme,
			}
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: namespace,
				},
			}
			// econcile for the first time err is resource not found
			_, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// create resource first adn then reconcile for the first time
			ssSpec := testutils.NewLicenseMaster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApi.LicenseManagerPausedAnnotation] = ""
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// reconcile after removing annotations for pause
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			// reconcile after adding delete timestamp
			Expect(err).ToNot(HaveOccurred())
			ssSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetLicenseMaster(name string, namespace string) (*enterpriseApi.LicenseMaster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting LicenseMaster custom resource to be created successfully")
	ss := &enterpriseApi.LicenseMaster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateLicenseMaster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.LicenseMaster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := &enterpriseApi.LicenseMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: enterpriseApi.LicenseMasterSpec{},
	}
	ssSpec = testutils.NewLicenseMaster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting LicenseMaster custom resource to be created successfully")
	ss := &enterpriseApi.LicenseMaster{}
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

func UpdateLicenseMaster(instance *enterpriseApi.LicenseMaster, status enterpriseApi.Phase) *enterpriseApi.LicenseMaster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewLicenseMaster(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting LicenseMaster custom resource to be created successfully")
	ss := &enterpriseApi.LicenseMaster{}
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

func DeleteLicenseMaster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting LicenseMaster Deleted successfully")
	Eventually(func() error {
		ssys := &enterpriseApi.LicenseMaster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
