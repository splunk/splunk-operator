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

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	common "github.com/splunk/splunk-operator/controllers/common"
	provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ClusterManagerReconciler reconciles a ClusterManager object
type ClusterManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
	reconcileCounters.With(getPrometheusLabels(req, "ClusterManager")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "ClusterManager")

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("clustermanager", req.NamespacedName)

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

	// If the reconciliation is paused, requeue
	annotations := instance.GetAnnotations()
	if annotations != nil {
		if _, ok := annotations[enterpriseApi.ClusterManagerPausedAnnotation]; ok {
			return ctrl.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
		}
	}

	reqLogger.Info("start", "CR version", instance.GetResourceVersion())

	/*
		// get credentials
			prov, err := provisioner.Factory.NewProvisioner(ctx, sad, info.publishEvent)
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to create provisioner")
			}
			stateMachine := newUpgradeClusterManagerStateMachine(ctx, instance, r, prov, true)
			actResult := stateMachine.ReconcileState(info)
			result, err = actResult.Result()

			if err != nil {
				err = errors.Wrap(err, fmt.Sprintf("action %q failed", initialState))
				return
			}

			// Only save status when we're told to, otherwise we
			// introduce an infinite loop reconciling the same object over and
			// over when there is an unrecoverable error (tracked through the
			// error state of the cm).
			if actResult.Dirty() {

				// Save CR
				info.log.Info("saving cluster manager status",
					"operational status", cm.OperationalStatus(),
					"provisioning state", cm.Status.Provisioning.State)
				err = r.saveCRStatus(cm)
				if err != nil {
					return ctrl.Result{}, errors.Wrap(err,
						fmt.Sprintf("failed to save cm status after %q", initialState))
				}

				for _, cb := range info.postSaveCallbacks {
					cb()
				}
			}

			for _, e := range info.events {
				r.publishEvent(request, e)
			}

			logResult(info, result)

	*/

	result, err := ApplyClusterManager(ctx, r.Client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		reqLogger.Info("Requeued", "period(seconds)", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

// ApplyClusterManager adding to handle unit test case
var ApplyClusterManager = func(ctx context.Context, client client.Client, instance *enterpriseApi.ClusterManager) (reconcile.Result, error) {
	return enterprise.ApplyClusterManager(ctx, client, instance)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.ClusterManager{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			common.LabelChangedPredicate(),
			common.SecretChangedPredicate(),
			common.StatefulsetChangedPredicate(),
			common.PodChangedPredicate(),
			common.ConfigMapChangedPredicate(),
			common.CrdChangedPredicate(),
		)).
		Watches(&source.Kind{Type: &appsv1.StatefulSet{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterpriseApi.ClusterManager{},
			}).
		Watches(&source.Kind{Type: &corev1.Secret{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterpriseApi.ClusterManager{},
			}).
		Watches(&source.Kind{Type: &corev1.Pod{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterpriseApi.ClusterManager{},
			}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}},
			&handler.EnqueueRequestForOwner{
				IsController: false,
				OwnerType:    &enterpriseApi.ClusterManager{},
			}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: enterpriseApi.TotalWorker,
		}).
		Complete(r)
}

// recordInstrumentionData Record api profiling information to prometheus
func recordInstrumentionData(start time.Time, req ctrl.Request, module string, name string) {
	metricLabels := getPrometheusLabels(req, name)
	metricLabels[labelModuleName] = module
	metricLabels[labelMethodName] = name
	value := float64(time.Since(start) / time.Millisecond)
	apiTotalTimeMetricEvents.With(metricLabels).Set(value)
}

// clearError removes any existing error message.
func clearError(resource *enterpriseApi.ClusterManager) (dirty bool) {
	dirty = resource.SetOperationalStatus(enterpriseApi.OperationalStatusOK)
	var emptyErrType enterpriseApi.ErrorType
	if resource.Status.ErrorType != emptyErrType {
		resource.Status.ErrorType = emptyErrType
		dirty = true
	}
	if resource.Status.ErrorMessage != "" {
		resource.Status.ErrorMessage = ""
		dirty = true
	}
	return dirty
}

func (r *ClusterManagerReconciler) actionClusterManagerPrepare(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	provResult, err := prov.CheckClusterManagerHealth(ctx)

	if err != nil {
		return actionError{errors.Wrap(err, "cluster manager is not in healthy state")}
	}

	if provResult.ErrorMessage != "" {
		return recordActionFailure(info, enterpriseApi.ClusterManagerPrepareUpgradeError, provResult.ErrorMessage)
	}

	if provResult.Dirty {
		result := actionContinue{provResult.RequeueAfter}
		if clearError(info.resource) {
			return actionUpdate{result}
		}
		return result
	}
	return actionComplete{}
}

func (r *ClusterManagerReconciler) actionClusterManagerBackup(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	return actionComplete{}
}

func (r *ClusterManagerReconciler) actionClusterManagerRestore(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	return actionComplete{}
}

func (r *ClusterManagerReconciler) actionClusterManagerUpgrade(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	return actionComplete{}
}

func (r *ClusterManagerReconciler) actionClusterManagerVerification(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	return actionComplete{}
}

func (r *ClusterManagerReconciler) actionClusterManagerReady(ctx context.Context, prov provisioner.Provisioner, info *reconcileCMInfo) actionResult {

	return actionComplete{}
}

// setErrorMessage updates the ErrorMessage in the host Status struct
// and increases the ErrorCount
func setErrorMessage(resource *enterpriseApi.ClusterManager, errType enterpriseApi.ErrorType, message string) {
	resource.Status.OperationalStatus = enterpriseApi.OperationalStatusError
	resource.Status.ErrorType = errType
	resource.Status.ErrorMessage = message
	resource.Status.ErrorCount++
}

func recordActionFailure(info *reconcileCMInfo, errorType enterpriseApi.ErrorType, errorMessage string) actionFailed {

	setErrorMessage(info.resource, errorType, errorMessage)

	eventType := map[enterpriseApi.ErrorType]string{
		enterpriseApi.LicenseManagerPrepareUpgradeError:    "PrepareForUpgradeError",
		enterpriseApi.LicenseManagerBackupError:            "BackupError",
		enterpriseApi.LicenseManagerRestoreError:           "RestoreError",
		enterpriseApi.LicenseManagerUpgradeError:           "UpgradeError",
		enterpriseApi.LicenseManagerVerificationError:      "VerificationError",
		enterpriseApi.ClusterManagerPrepareUpgradeError:    "PrepareForUpgradeError",
		enterpriseApi.ClusterManageBackupError:             "BackupError",
		enterpriseApi.ClusterManageRestoreError:            "RestoreError",
		enterpriseApi.ClusterManageUpgradeError:            "UpgradeError",
		enterpriseApi.ClusterManageVerificationError:       "VerificationError",
		enterpriseApi.MonitoringConsolePrepareUpgradeError: "PrepareForUpgradeError",
		enterpriseApi.MonitoringConsoleBackupError:         "BackupError",
		enterpriseApi.MonitoringConsoleRestoreError:        "RestoreError",
		enterpriseApi.MonitoringConsoleUpgradeError:        "UpgradeError",
		enterpriseApi.MonitoringConsoleVerificationError:   "VerificationError",

		enterpriseApi.SearchHeadClusterPrepareUpgradeError: "PrepareForUpgradeError",
		enterpriseApi.SearchHeadClusterBackupError:         "BackupError",
		enterpriseApi.SearchHeadClusterRestoreError:        "RestoreError",
		enterpriseApi.SearchHeadClusterUpgradeError:        "UpgradeError",
		enterpriseApi.SearchHeadClusterVerificationError:   "VerificationError",
		enterpriseApi.IndexerClusterPrepareUpgradeError:    "PrepareForUpgradeError",
		enterpriseApi.IndexerClusterBackupError:            "BackupError",
		enterpriseApi.IndexerClusterRestoreError:           "RestoreError",
		enterpriseApi.IndexerClusterUpgradeError:           "UpgradeError",
		enterpriseApi.IndexerClusterVerificationError:      "VerificationError",
		enterpriseApi.StandalonePrepareUpgradeError:        "UpgradeError",
		enterpriseApi.StandaloneBackupError:                "BackupError",
		enterpriseApi.StandaloneRestoreError:               "RestoreError",
		enterpriseApi.StandaloneUpgradeError:               "UpgradeError",
		enterpriseApi.StandaloneVerificationError:          "VerificationError",
	}[errorType]

	counter := actionFailureCounters.WithLabelValues(eventType)
	info.postSaveCallbacks = append(info.postSaveCallbacks, counter.Inc)

	info.publishEvent("Error", eventType, errorMessage)

	return actionFailed{dirty: true, ErrorType: errorType, errorCount: info.resource.Status.ErrorCount}
}
