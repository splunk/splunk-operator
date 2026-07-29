package controller

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/internal/controller/testutils"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
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

		It("routes a deleting paused SearchHeadCluster to finalization without a status write", func() {
			ctx := context.Background()
			now := metav1.Now()
			searchHeadCluster := testutils.NewSearchHeadCluster(
				"deleting-paused",
				"ns-splunk-shc-deleting-paused",
				"image",
			)
			searchHeadCluster.Annotations = map[string]string{
				enterpriseApi.SearchHeadClusterPausedAnnotation: "true",
			}
			searchHeadCluster.DeletionTimestamp = &now
			searchHeadCluster.Finalizers = []string{
				"enterprise.splunk.com/delete-pvc",
			}

			statusUpdates := 0
			isolatedClient := fake.NewClientBuilder().
				WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
				WithObjects(searchHeadCluster).
				WithInterceptorFuncs(interceptor.Funcs{
					SubResourceUpdate: func(
						ctx context.Context,
						c client.Client,
						subResourceName string,
						obj client.Object,
						opts ...client.SubResourceUpdateOption,
					) error {
						statusUpdates++
						return c.SubResource(subResourceName).Update(ctx, obj, opts...)
					},
				}).
				Build()
			reconciler := &SearchHeadClusterReconciler{
				Client: isolatedClient,
				Scheme: scheme.Scheme,
			}

			originalApplySearchHeadCluster := ApplySearchHeadCluster
			DeferCleanup(func() {
				ApplySearchHeadCluster = originalApplySearchHeadCluster
			})
			applyCalls := 0
			ApplySearchHeadCluster = func(
				context.Context,
				client.Client,
				*enterpriseApi.SearchHeadCluster,
			) (reconcile.Result, error) {
				applyCalls++
				return reconcile.Result{}, nil
			}

			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      searchHeadCluster.Name,
					Namespace: searchHeadCluster.Namespace,
				},
			}
			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(applyCalls).To(Equal(1))
			Expect(statusUpdates).To(BeZero())
		})

		It("resumes a persisted authorized rolling partition after reconciler reconstruction", func() {
			ctx := context.Background()
			namespace := "ns-splunk-shc-rollout-resume"
			name := "rollout-resume"
			statefulSetName := "splunk-" + name + "-search-head"
			key := types.NamespacedName{Name: name, Namespace: namespace}
			statefulSetKey := types.NamespacedName{
				Name:      statefulSetName,
				Namespace: namespace,
			}

			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(ctx, nsSpecs)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(context.Background(), nsSpecs)
			})

			searchHeadCluster := testutils.NewSearchHeadCluster(name, namespace, "image")
			searchHeadCluster.Spec.Replicas = 3
			searchHeadCluster.Spec.LifecyclePolicy = &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			}
			searchHeadCluster.Annotations = map[string]string{
				enterpriseApi.SearchHeadClusterPausedAnnotation: "true",
			}
			Expect(k8sClient.Create(ctx, searchHeadCluster)).To(Succeed())

			targetOrdinal := int32(2)
			authorizedAt := metav1.Now()
			Eventually(func() error {
				current := &enterpriseApi.SearchHeadCluster{}
				if err := k8sClient.Get(ctx, key, current); err != nil {
					return err
				}
				current.Status.Phase = enterpriseApi.PhaseUpdating
				current.Status.DeployerPhase = enterpriseApi.PhaseUpdating
				current.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
					OperationID:             "pod-update-2",
					Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
					DesiredRevision:         "revision-2",
					TargetPod:               statefulSetName + "-2",
					TargetOrdinal:           &targetOrdinal,
					Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
					ReplacementAuthorizedAt: &authorizedAt,
				}
				return k8sClient.Status().Update(ctx, current)
			}, timeout, interval).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &enterpriseApi.SearchHeadCluster{}
				g.Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
				pausedCondition := meta.FindStatusCondition(
					current.Status.Conditions,
					string(enterpriseApi.ConditionPaused),
				)
				g.Expect(pausedCondition).NotTo(BeNil())
				g.Expect(pausedCondition.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			replicas := int32(3)
			initialPartition := int32(3)
			labels := map[string]string{"app": "shc-rollout-resume"}
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetName,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: statefulSetName + "-headless",
					Replicas:    &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: labels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "splunk",
								Image: "image",
							}},
						},
					},
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
						RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
							Partition: &initialPartition,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			originalApplySearchHeadCluster := ApplySearchHeadCluster
			DeferCleanup(func() {
				ApplySearchHeadCluster = originalApplySearchHeadCluster
			})

			reconcileCount := 0
			ApplySearchHeadCluster = func(
				ctx context.Context,
				controllerClient client.Client,
				instance *enterpriseApi.SearchHeadCluster,
			) (reconcile.Result, error) {
				reconcileCount++
				operation := instance.Status.LifecycleOperation
				if operation == nil ||
					operation.TargetOrdinal == nil ||
					*operation.TargetOrdinal != targetOrdinal ||
					operation.DesiredRevision != "revision-2" ||
					operation.ReplacementAuthorizedAt == nil {
					return reconcile.Result{}, fmt.Errorf(
						"persisted lifecycle authorization was not reconstructed: %#v",
						operation,
					)
				}

				current := &appsv1.StatefulSet{}
				if err := controllerClient.Get(ctx, statefulSetKey, current); err != nil {
					return reconcile.Result{}, err
				}
				if current.Spec.UpdateStrategy.RollingUpdate == nil ||
					current.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
					return reconcile.Result{}, fmt.Errorf("rolling partition is absent")
				}

				partition := *current.Spec.UpdateStrategy.RollingUpdate.Partition
				switch reconcileCount {
				case 1:
					if partition != initialPartition {
						return reconcile.Result{}, fmt.Errorf(
							"initial partition = %d, want %d",
							partition,
							initialPartition,
						)
					}
					revised := current.DeepCopy()
					authorizedPartition := targetOrdinal
					revised.Spec.UpdateStrategy.RollingUpdate.Partition = &authorizedPartition
					_, err := splctrl.ApplyStatefulSet(ctx, controllerClient, revised)
					return reconcile.Result{Requeue: true}, err
				default:
					if partition != targetOrdinal {
						return reconcile.Result{}, fmt.Errorf(
							"resumed partition = %d, want %d",
							partition,
							targetOrdinal,
						)
					}
					return reconcile.Result{}, nil
				}
			}

			request := reconcile.Request{NamespacedName: key}
			isolatedClient := &unpausedSearchHeadClusterClient{Client: k8sClient}
			firstReconciler := &SearchHeadClusterReconciler{
				Client: isolatedClient,
				Scheme: scheme.Scheme,
			}
			result, err := firstReconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			persistedStatefulSet := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, statefulSetKey, persistedStatefulSet)).To(Succeed())
			Expect(persistedStatefulSet.Spec.UpdateStrategy.RollingUpdate).NotTo(BeNil())
			Expect(persistedStatefulSet.Spec.UpdateStrategy.RollingUpdate.Partition).NotTo(BeNil())
			Expect(*persistedStatefulSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal(targetOrdinal))

			// A new reconciler has no in-memory knowledge of the previous pass. It
			// must recover the authorization checkpoint and partition from the API.
			reconstructedReconciler := &SearchHeadClusterReconciler{
				Client: isolatedClient,
				Scheme: scheme.Scheme,
			}
			result, err = reconstructedReconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(reconcileCount).To(Equal(2))

			persistedStatefulSet = &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, statefulSetKey, persistedStatefulSet)).To(Succeed())
			Expect(*persistedStatefulSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal(targetOrdinal))

			persistedSearchHeadCluster := &enterpriseApi.SearchHeadCluster{}
			Expect(k8sClient.Get(ctx, key, persistedSearchHeadCluster)).To(Succeed())
			Expect(persistedSearchHeadCluster.Status.LifecycleOperation).NotTo(BeNil())
			Expect(persistedSearchHeadCluster.Status.LifecycleOperation.OperationID).To(Equal("pod-update-2"))
			Expect(persistedSearchHeadCluster.Status.LifecycleOperation.ReplacementAuthorizedAt).NotTo(BeNil())

			pods := &corev1.PodList{}
			Expect(k8sClient.List(ctx, pods, client.InNamespace(namespace))).To(Succeed())
			Expect(pods.Items).To(BeEmpty())
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

	})
})

// unpausedSearchHeadClusterClient keeps the shared envtest manager from
// reconciling this fixture while allowing explicitly constructed reconcilers
// to exercise the normal, unpaused path.
type unpausedSearchHeadClusterClient struct {
	client.Client
}

func (c *unpausedSearchHeadClusterClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	if err := c.Client.Get(ctx, key, object, opts...); err != nil {
		return err
	}
	if searchHeadCluster, ok := object.(*enterpriseApi.SearchHeadCluster); ok {
		delete(searchHeadCluster.Annotations, enterpriseApi.SearchHeadClusterPausedAnnotation)
	}
	return nil
}

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
	Eventually(func() bool {
		return k8sClient.Get(context.Background(), key, ss) == nil
	}, timeout, interval).Should(BeTrue())
	if status != "" {
		ss.Status.Phase = status
		ss.Status.DeployerPhase = status
		Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
	}

	return ss
}

func UpdateSearchHeadCluster(instance *enterpriseApi.SearchHeadCluster, status enterpriseApi.Phase) *enterpriseApi.SearchHeadCluster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	ssSpec := testutils.NewSearchHeadCluster(instance.Name, instance.Namespace, "image")
	ssSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), ssSpec)).Should(Succeed())

	By("Expecting SearchHeadCluster custom resource to be updated successfully")
	ss := &enterpriseApi.SearchHeadCluster{}
	Eventually(func() bool {
		return k8sClient.Get(context.Background(), key, ss) == nil
	}, timeout, interval).Should(BeTrue())
	if status != "" {
		ss.Status.Phase = status
		ss.Status.DeployerPhase = status
		Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
	}

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
