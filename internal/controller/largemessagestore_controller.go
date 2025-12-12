/*
Copyright 2025.

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
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// LargeMessageStoreReconciler reconciles a LargeMessageStore object
type LargeMessageStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=largemessagestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=largemessagestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=largemessagestores/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the LargeMessageStore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *LargeMessageStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "LargeMessageStore")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "LargeMessageStore")

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("largemessagestore", req.NamespacedName)

	// Fetch the LargeMessageStore
	instance := &enterpriseApi.LargeMessageStore{}
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
		return ctrl.Result{}, errors.Wrap(err, "could not load largemessagestore data")
	}

	// If the reconciliation is paused, requeue
	annotations := instance.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[enterpriseApi.LargeMessageStorePausedAnnotation]; ok {
			return ctrl.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
		}
	}

	reqLogger.Info("start", "CR version", instance.GetResourceVersion())

	result, err := ApplyLargeMessageStore(ctx, r.Client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		reqLogger.Info("Requeued", "period(seconds)", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

var ApplyLargeMessageStore = func(ctx context.Context, client client.Client, instance *enterpriseApi.LargeMessageStore) (reconcile.Result, error) {
	return enterprise.ApplyLargeMessageStore(ctx, client, instance)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LargeMessageStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.LargeMessageStore{}).
		WithEventFilter(predicate.Or(
			common.GenerationChangedPredicate(),
			common.AnnotationChangedPredicate(),
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.StatefulsetChangedPredicate(),
			common.PodChangedPredicate(),
			common.CrdChangedPredicate(),
		)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		}).
		Complete(r)
}
