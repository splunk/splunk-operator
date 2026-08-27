// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"log/slog"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/common"
	"github.com/splunk/splunk-operator/pkg/logging"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// NoahClusterReconciler reconciles a NoahCluster object
type NoahClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=noahclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=noahclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=noahclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NoahClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "NoahCluster")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "NoahCluster")

	logger := slog.Default().With("controller", "NoahCluster", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	// Fetch the NoahCluster
	instance := &enterpriseApi.NoahCluster{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If the reconciliation is paused, set the Paused condition and requeue
	if instance.GetAnnotations()[enterpriseApi.NoahClusterPausedAnnotation] == "true" {
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

	// TODO(user): Implement reconciliation logic for NoahCluster
	// This is a placeholder — add your business logic here.
	result := reconcile.Result{}

	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NoahClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.NoahCluster{}).
		WithEventFilter(predicate.Or(
			common.GenerationChangedPredicate(),
			common.AnnotationChangedPredicate(),
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
		)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		}).
		Named("noah-cluster-controller").
		Complete(r)
}
