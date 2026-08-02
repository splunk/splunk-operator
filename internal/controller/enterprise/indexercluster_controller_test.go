package controller

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

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
)

var _ = Describe("IndexerCluster Controller", Label("integration"), func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("IndexerCluster Management", func() {

		It("Get IndexerCluster custom resource should failed", func() {
			namespace := "ns-splunk-ic-1"
			ApplyIndexerCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetIndexerCluster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("indexerclusters.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IndexerCluster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-ic-2"
			ApplyIndexerCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApi.IndexerClusterPausedAnnotation] = "true"
			CreateIndexerCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetIndexerCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			ssSpec.Status.ClusterManagerPhase, ssSpec.Status.ClusterMasterPhase = "Ready", "Ready" //CM* Phase can't be empty
			UpdateIndexerCluster(ssSpec, enterpriseApi.PhaseReady)
			DeleteIndexerCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IndexerCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-ic-3"
			ApplyIndexerCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateIndexerCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteIndexerCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-ic-4"
			ApplyIndexerCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IndexerCluster{}).WithObjects(nsSpecs)
			c := builder.Build()
			instance := IndexerClusterReconciler{
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
			ssSpec := testutils.NewIndexerCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApi.IndexerClusterPausedAnnotation] = "true"
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
			namespace := "ns-splunk-ic-stalled"
			ctx := context.TODO()
			nsSpec := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IndexerCluster{}).WithObjects(nsSpec)
			c := builder.Build()
			recorder := record.NewFakeRecorder(10)
			reconciler := IndexerClusterReconciler{
				Client:   c,
				Scheme:   scheme.Scheme,
				Recorder: recorder,
			}
			ssSpec := testutils.NewIndexerCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())

			ApplyIndexerCluster = func(ctx context.Context, cl client.Client, instance *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, splcommon.NewTerminalError("ValidateSpecFailed", "test terminal failure", fmt.Errorf("missing ClusterManagerRef"))
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

func GetIndexerCluster(name string, namespace string) (*enterpriseApi.IndexerCluster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateIndexerCluster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.IndexerCluster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := testutils.NewIndexerCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.ClusterManagerPhase, ss.Status.ClusterMasterPhase = status, status //CM* Phase can't be empty
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func UpdateIndexerCluster(instance *enterpriseApi.IndexerCluster, status enterpriseApi.Phase) *enterpriseApi.IndexerCluster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewIndexerCluster(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.ClusterManagerPhase, ss.Status.ClusterMasterPhase = status, status //CM* Phase can't be empty
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
		ssys := &enterpriseApi.IndexerCluster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
