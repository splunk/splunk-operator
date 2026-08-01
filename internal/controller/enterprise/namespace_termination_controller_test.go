package controller

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type terminatingNamespaceControllerCase struct {
	name            string
	pauseAnnotation string
	object          client.Object
	reconcile       func(context.Context, client.Client, reconcile.Request) (reconcile.Result, error)
	stubApply       func(*int, error) func()
}

var _ = Describe("Namespace termination controller guard", func() {
	namespace := "shc90-terminating"
	queue := &enterpriseApi.Queue{ObjectMeta: metav1.ObjectMeta{Name: "queue", Namespace: namespace}}
	objectStorage := &enterpriseApi.ObjectStorage{ObjectMeta: metav1.ObjectMeta{Name: "storage", Namespace: namespace}}

	cases := []terminatingNamespaceControllerCase{
		{
			name:            "Standalone",
			pauseAnnotation: enterpriseApi.StandalonePausedAnnotation,
			object:          testutils.NewStandalone("standalone", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&StandaloneReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyStandalone
				ApplyStandalone = func(context.Context, client.Client, *enterpriseApi.Standalone) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyStandalone = original }
			},
		},
		{
			name:            "LicenseManager",
			pauseAnnotation: enterpriseApi.LicenseManagerPausedAnnotation,
			object:          testutils.NewLicenseManager("license-manager", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&LicenseManagerReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyLicenseManager
				ApplyLicenseManager = func(context.Context, client.Client, *enterpriseApi.LicenseManager) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyLicenseManager = original }
			},
		},
		{
			name:            "ClusterManager",
			pauseAnnotation: enterpriseApi.ClusterManagerPausedAnnotation,
			object:          testutils.NewClusterManager("cluster-manager", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&ClusterManagerReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyClusterManager
				ApplyClusterManager = func(context.Context, client.Client, *enterpriseApi.ClusterManager, splutil.PodExecClientImpl) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyClusterManager = original }
			},
		},
		{
			name:            "MonitoringConsole",
			pauseAnnotation: enterpriseApi.MonitoringConsolePausedAnnotation,
			object:          testutils.NewMonitoringConsole("monitoring-console", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&MonitoringConsoleReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyMonitoringConsole
				ApplyMonitoringConsole = func(context.Context, client.Client, *enterpriseApi.MonitoringConsole) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyMonitoringConsole = original }
			},
		},
		{
			name:            "IndexerCluster",
			pauseAnnotation: enterpriseApi.IndexerClusterPausedAnnotation,
			object:          testutils.NewIndexerCluster("indexer-cluster", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&IndexerClusterReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyIndexerCluster
				ApplyIndexerCluster = func(context.Context, client.Client, *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyIndexerCluster = original }
			},
		},
		{
			name:            "SearchHeadCluster",
			pauseAnnotation: enterpriseApi.SearchHeadClusterPausedAnnotation,
			object:          testutils.NewSearchHeadCluster("search-head-cluster", namespace, "image"),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&SearchHeadClusterReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplySearchHeadCluster
				ApplySearchHeadCluster = func(context.Context, client.Client, *enterpriseApi.SearchHeadCluster) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplySearchHeadCluster = original }
			},
		},
		{
			name:            "IngestorCluster",
			pauseAnnotation: enterpriseApi.IngestorClusterPausedAnnotation,
			object:          testutils.NewIngestorCluster("ingestor-cluster", namespace, "image", objectStorage, queue),
			reconcile: func(ctx context.Context, c client.Client, req reconcile.Request) (reconcile.Result, error) {
				return (&IngestorClusterReconciler{Client: c}).Reconcile(ctx, req)
			},
			stubApply: func(calls *int, applyErr error) func() {
				original := ApplyIngestorCluster
				ApplyIngestorCluster = func(context.Context, client.Client, *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
					*calls++
					return reconcile.Result{}, applyErr
				}
				return func() { ApplyIngestorCluster = original }
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		It("stops "+tc.name+" before Apply or status mutation", func() {
			ctx := context.Background()
			now := metav1.Now()
			terminatingNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"kubernetes"},
				},
				Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
			}
			object := tc.object.DeepCopyObject().(client.Object)
			statusUpdates := 0
			isolatedClient := fake.NewClientBuilder().
				WithScheme(clientgoscheme.Scheme).
				WithStatusSubresource(object).
				WithObjects(terminatingNamespace, object).
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

			applyCalls := 0
			restore := tc.stubApply(&applyCalls, nil)
			DeferCleanup(restore)
			request := reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      object.GetName(),
				Namespace: object.GetNamespace(),
			}}

			result, err := tc.reconcile(ctx, isolatedClient, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(applyCalls).To(BeZero())
			Expect(statusUpdates).To(BeZero())
		})

		It("allows "+tc.name+" deletion finalization in a terminating namespace", func() {
			ctx := context.Background()
			now := metav1.Now()
			terminatingNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"kubernetes"},
				},
				Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
			}
			object := tc.object.DeepCopyObject().(client.Object)
			object.SetDeletionTimestamp(&now)
			object.SetAnnotations(map[string]string{tc.pauseAnnotation: "true"})
			if len(object.GetFinalizers()) == 0 {
				object.SetFinalizers([]string{"enterprise.splunk.com/delete-pvc"})
			}
			statusUpdates := 0
			isolatedClient := fake.NewClientBuilder().
				WithScheme(clientgoscheme.Scheme).
				WithStatusSubresource(object).
				WithObjects(terminatingNamespace, object).
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

			applyCalls := 0
			restore := tc.stubApply(&applyCalls, nil)
			DeferCleanup(restore)
			request := reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      object.GetName(),
				Namespace: object.GetNamespace(),
			}}

			_, err := tc.reconcile(ctx, isolatedClient, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(applyCalls).To(Equal(1))
			Expect(statusUpdates).To(BeZero())
		})

		It("preserves "+tc.name+" deletion finalization failures for status handling", func() {
			ctx := context.Background()
			now := metav1.Now()
			terminatingNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"kubernetes"},
				},
				Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
			}
			object := tc.object.DeepCopyObject().(client.Object)
			object.SetDeletionTimestamp(&now)
			object.SetAnnotations(map[string]string{tc.pauseAnnotation: "true"})
			if len(object.GetFinalizers()) == 0 {
				object.SetFinalizers([]string{"enterprise.splunk.com/delete-pvc"})
			}
			statusUpdates := 0
			isolatedClient := fake.NewClientBuilder().
				WithScheme(clientgoscheme.Scheme).
				WithStatusSubresource(object).
				WithObjects(terminatingNamespace, object).
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

			applyCalls := 0
			applyErr := errors.New("finalization failed")
			restore := tc.stubApply(&applyCalls, applyErr)
			DeferCleanup(restore)
			request := reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      object.GetName(),
				Namespace: object.GetNamespace(),
			}}

			_, err := tc.reconcile(ctx, isolatedClient, request)
			Expect(err).To(MatchError(applyErr))
			Expect(applyCalls).To(Equal(1))
			Expect(statusUpdates).To(Equal(1))
		})

		It("treats "+tc.name+" NamespaceTerminating admission as expected cancellation", func() {
			ctx := context.Background()
			activeNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			object := tc.object.DeepCopyObject().(client.Object)
			statusUpdates := 0
			isolatedClient := fake.NewClientBuilder().
				WithScheme(clientgoscheme.Scheme).
				WithStatusSubresource(object).
				WithObjects(activeNamespace, object).
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

			applyCalls := 0
			restore := tc.stubApply(&applyCalls, newNamespaceTerminatingAdmissionError())
			DeferCleanup(restore)
			request := reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      object.GetName(),
				Namespace: object.GetNamespace(),
			}}

			result, err := tc.reconcile(ctx, isolatedClient, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(applyCalls).To(Equal(1))
			Expect(statusUpdates).To(BeZero())
		})
	}
})
