package controller

import (
	"context"

	"github.com/splunk/splunk-operator/internal/controller/testutils"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

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
	"k8s.io/client-go/util/retry"

	"fmt"

	"github.com/pkg/errors"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

var _ = Describe("SearchHeadCluster Controller", Label("integration"), func() {

	AfterEach(func() {

	})

	Context("SearchHeadCluster Management", func() {

		It("Get SearchHeadCluster custom resource should failed", func() {
			namespace := "ns-splunk-shc-1"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
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
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApi.SearchHeadClusterPausedAnnotation] = "true"
			CreateSearchHeadCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetSearchHeadCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.DeployerPhase = "Ready"
			ssSpec.Status.Phase = "Ready"
			UpdateSearchHeadCluster(ssSpec, enterpriseApi.PhaseReady)
			DeleteSearchHeadCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create SearchHeadCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-shc-3"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateSearchHeadCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteSearchHeadCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-shc-4"
			ApplySearchHeadCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
			c := builder.Build()
			instance := SearchHeadClusterReconciler{
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
			ssSpec := testutils.NewSearchHeadCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApi.SearchHeadClusterPausedAnnotation] = "true"
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=True condition was written
			Expect(c.Get(ctx, request.NamespacedName, ssSpec)).Should(Succeed())
			pausedCond := meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))
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
			namespace := "ns-splunk-shc-stalled"
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
			c := builder.Build()
			recorder := record.NewFakeRecorder(10)
			reconciler := SearchHeadClusterReconciler{
				Client:   c,
				Scheme:   scheme.Scheme,
				Recorder: recorder,
			}
			ssSpec := testutils.NewSearchHeadCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())

			ApplySearchHeadCluster = func(ctx context.Context, cl client.Client, instance *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
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

func GetSearchHeadCluster(name string, namespace string) (*enterpriseApi.SearchHeadCluster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting SearchHeadCluster custom resource to be created successfully")
	ss := &enterpriseApi.SearchHeadCluster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateSearchHeadCluster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.SearchHeadCluster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := testutils.NewSearchHeadCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())

	By("Expecting SearchHeadCluster custom resource to be created successfully")
	ss := &enterpriseApi.SearchHeadCluster{}
	Eventually(func() error {
		if err := k8sClient.Get(context.Background(), key, ss); err != nil {
			return err
		}
		if status != "" {
			ss.Status.Phase = status
			ss.Status.DeployerPhase = status
			return k8sClient.Status().Update(context.Background(), ss)
		}
		return nil
	}, timeout, interval).Should(Succeed())

	return ss
}

func UpdateSearchHeadCluster(instance *enterpriseApi.SearchHeadCluster, status enterpriseApi.Phase) *enterpriseApi.SearchHeadCluster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &enterpriseApi.SearchHeadCluster{}
		if err := k8sClient.Get(context.Background(), key, current); err != nil {
			return err
		}
		ssSpec := testutils.NewSearchHeadCluster(instance.Name, instance.Namespace, "image")
		ssSpec.ResourceVersion = current.ResourceVersion
		return k8sClient.Update(context.Background(), ssSpec)
	})).Should(Succeed())

	By("Expecting SearchHeadCluster custom resource to be updated successfully")
	ss := &enterpriseApi.SearchHeadCluster{}
	Eventually(func() error {
		if err := k8sClient.Get(context.Background(), key, ss); err != nil {
			return err
		}
		if status != "" {
			ss.Status.Phase = status
			ss.Status.DeployerPhase = status
			return k8sClient.Status().Update(context.Background(), ss)
		}
		return nil
	}, timeout, interval).Should(Succeed())

	return ss
}

func DeleteSearchHeadCluster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting SearchHeadCluster Deleted successfully")
	Eventually(func() error {
		ssys := &enterpriseApi.SearchHeadCluster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
