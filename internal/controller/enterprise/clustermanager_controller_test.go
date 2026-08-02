package controller

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/internal/controller/testutils"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"

	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/pkg/errors"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

var _ = Describe("ClusterManager Controller", Label("integration"), func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("ClusterManager Management failed", func() {

		It("Get ClusterManager custom resource should failed", func() {
			namespace := "ns-splunk-cm-1"
			ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetClusterManager("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("clustermanagers.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})

	Context("ClusterManager Management with annotations", func() {

		It("Create ClusterManager custom resource with annotations should pause", func() {
			namespace := "ns-splunk-cm-2"
			ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApi.ClusterManagerPausedAnnotation] = "true"
			CreateClusterManager("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetClusterManager("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			UpdateClusterManager(ssSpec, enterpriseApi.PhaseReady)
			DeleteClusterManager("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})
	Context("ClusterManager Management", func() {
		It("Create ClusterManager custom resource should succeeded", func() {
			namespace := "ns-splunk-cm-3"
			ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateClusterManager("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteClusterManager("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-cm-4"
			ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.ClusterManager{}).WithObjects(nsSpecs)
			c := builder.Build()
			instance := ClusterManagerReconciler{
				Client: c,
				Scheme: scheme.Scheme,
			}
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: namespace,
				},
			}
			// reconcile for the first time err is resource not found
			_, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// create resource first and then reconcile for the first time
			ssSpec := testutils.NewClusterManager("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApi.ClusterManagerPausedAnnotation] = "true"
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			result, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())
			// verify Paused=True condition was written
			Expect(c.Get(ctx, request.NamespacedName, ssSpec)).Should(Succeed())
			Expect(ssSpec.Status.Phase).To(Equal(enterpriseApi.PhasePending))
			Expect(ssSpec.Status.ObservedGeneration).To(Equal(ssSpec.Generation))
			pausedCond := meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))
			progressingCond := meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionProgressing))
			Expect(progressingCond).ToNot(BeNil())
			Expect(progressingCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(progressingCond.Reason).To(Equal(string(enterpriseApi.ReasonPausedByAnnotation)))
			// reconcile after removing annotations for pause
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=False condition was written
			Expect(c.Get(ctx, request.NamespacedName, ssSpec)).Should(Succeed())
			pausedCond = meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionFalse))
			ssSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Reconcile emits Stalled Warning on every terminal failure reconcile", func() {
			namespace := "ns-splunk-cm-stalled"
			ctx := context.TODO()
			nsSpec := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.ClusterManager{}).WithObjects(nsSpec)
			c := builder.Build()
			recorder := record.NewFakeRecorder(10)
			reconciler := ClusterManagerReconciler{
				Client:   c,
				Scheme:   scheme.Scheme,
				Recorder: recorder,
			}
			ssSpec := testutils.NewClusterManager("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())

			ApplyClusterManager = func(ctx context.Context, cl client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
				return reconcile.Result{}, splcommon.NewTerminalError("ValidateSpecFailed", "test terminal failure", fmt.Errorf("test"))
			}

			request := reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: namespace},
			}

			// First reconcile: Stalled=False → Stalled=True — Stalled event expected
			_, err := reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + enterprise.EventReasonStalled + ` `)))

			// Second reconcile: Stalled=True → Stalled=True — Warning fires on every stalled reconcile
			_, err = reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + enterprise.EventReasonStalled + ` `)))
		})

	})
})

func GetClusterManager(name string, namespace string) (*enterpriseApi.ClusterManager, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting ClusterManager custom resource to be created successfully")
	ss := &enterpriseApi.ClusterManager{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateClusterManager(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.ClusterManager {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := testutils.NewClusterManager(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting ClusterManager custom resource to be created successfully")
	ss := &enterpriseApi.ClusterManager{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, NodeTimeout(timeout), interval).Should(BeTrue())

	return ss
}

func UpdateClusterManager(instance *enterpriseApi.ClusterManager, status enterpriseApi.Phase) *enterpriseApi.ClusterManager {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewClusterManager(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting ClusterManager custom resource to be created successfully")
	ss := &enterpriseApi.ClusterManager{}
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
		ssys := &enterpriseApi.ClusterManager{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
