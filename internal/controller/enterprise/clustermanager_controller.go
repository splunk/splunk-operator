/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"log/slog"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	"github.com/pkg/errors"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/config"
	certs "github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// ClusterManagerReconciler reconciles a ClusterManager object
type ClusterManagerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=clustermanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=clustermanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=clustermanagers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ClusterManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// your logic here
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "ClusterManager")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "ClusterManager")

	logger := slog.Default().With("controller", "ClusterManager", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	// Fetch the ClusterManager
	instance := &enterpriseApi.ClusterManager{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request.  Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers.  Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, errors.Wrap(err, "could not load cluster manager data")
	}

	// If the reconciliation is paused, set the Paused condition and requeue
	if instance.GetAnnotations()[enterpriseApi.ClusterManagerPausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
	} else if cond := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); cond != nil && cond.Status == metav1.ConditionTrue {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: false, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update unpaused status", "error", err)
			return ctrl.Result{}, err
		}
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())

	// Pass event recorder through context
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, r.Recorder)

	result, err := ApplyClusterManager(ctx, r.Client, instance, nil)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}
	fresh := &enterpriseApi.ClusterManager{}
	if fetchErr := r.Get(ctx, req.NamespacedName, fresh); fetchErr != nil {
		if k8serrors.IsNotFound(fetchErr) {
			return result, nil
		}
		logger.WarnContext(ctx, "failed to refetch CR for stalled condition update", "error", fetchErr)
		return result, fetchErr
	}
	oldConditions := append([]metav1.Condition(nil), fresh.Status.Conditions...)
	if msg, ok := splcommon.TerminalMessage(err); ok {
		reason, _ := splcommon.TerminalReason(err)
		fresh.Status.Conditions = splcommon.UpsertStalledCondition(fresh.Status.Conditions, reason, msg, fresh.GetGeneration())
	} else {
		fresh.Status.Conditions = splcommon.ClearStalledCondition(fresh.Status.Conditions, fresh.GetGeneration())
	}
	ep, epErr := enterprise.NewK8EventPublisherWithRecorder(r.Recorder, fresh)
	if epErr != nil {
		logger.WarnContext(ctx, "failed to create event publisher", "error", epErr)
		return result, epErr
	}
	enterprise.EmitStalledTransitionEvents(ctx, ep, fresh.GetName(), oldConditions, fresh.Status.Conditions)
	if updateErr := r.Status().Update(ctx, fresh); updateErr != nil {
		logger.WarnContext(ctx, "failed to upsert stalled condition", "error", updateErr)
		return result, updateErr
	}
	if _, ok := splcommon.TerminalMessage(err); ok {
		return reconcile.Result{}, err
	}
	return result, err
}

// ApplyClusterManager adding to handle unit test case
var ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) (reconcile.Result, error) {
	return enterprise.ApplyClusterManager(ctx, client, instance, podExecClient)
}

func (r *ClusterManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bldr := ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.ClusterManager{}).
		WithEventFilter(predicate.Or(
			common.GenerationChangedPredicate(),
			common.AnnotationChangedPredicate(),
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.StatefulsetChangedPredicate(),
			common.PodChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.CrdChangedPredicate(),
		)).
		Watches(&appsv1.StatefulSet{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.ClusterManager{},
			)).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.ClusterManager{},
			)).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.ClusterManager{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&enterpriseApi.ClusterManager{},
			)).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				cm, ok := obj.(*corev1.ConfigMap)
				if !ok {
					return nil
				}
				var list enterpriseApi.ClusterManagerList
				if err := r.Client.List(ctx, &list, client.InNamespace(cm.Namespace)); err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, cr := range list.Items {
					for _, vol := range cr.Spec.Volumes {
						if common.VolumeReferencesConfigMap(vol, cm.Name) {
							reqs = append(reqs, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name:      cr.Name,
									Namespace: cr.Namespace,
								},
							})
							break
						}
					}
				}
				return reqs
			}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		})

	if config.DefaultMutableFeatureGate.Enabled(config.CertManagement) {
		bldr = bldr.Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				certs.CertSecretMapper(mgr.GetClient(), &enterpriseApi.ClusterManagerList{})))
	}

	return bldr.
		Named("cluster-manager-controller").
		Complete(r)
}

// recordInstrumentionData Record api profiling information to prometheus
func recordInstrumentionData(start time.Time, req ctrl.Request, module string, name string) {
	metricLabels := metrics.GetPrometheusLabels(req, name)
	metricLabels[metrics.LabelModuleName] = module
	metricLabels[metrics.LabelMethodName] = name
	value := float64(time.Since(start) / time.Millisecond)
	metrics.ApiTotalTimeMetricEvents.With(metricLabels).Set(value)
}
