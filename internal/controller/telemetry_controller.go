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
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	"github.com/pkg/errors"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// TODO: Below two contants are defined at default/kustomizatio.yaml, need to get it programatically?
	ConfigMapNamePrefix = "splunk-operator-"
	ConfigMapLabelName  = "splunk-operator"

	telemetryRetryDelay = time.Second * 60
)

// TelemetryReconciler periodically reads all keys under the "telemetry" configmap
// in the Splunk operator namespace and logs all key values.
type TelemetryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

func (r *TelemetryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "Telemetry")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "Telemetry")

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("telemetry", req.NamespacedName)

	reqLogger.Info("Reconciling telemetry")

	// Fetch the ConfigMap
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, cm)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			reqLogger.Info("telemetry configmap not found; requeueing", "period(seconds)", int(telemetryRetryDelay/time.Second))
			return ctrl.Result{Requeue: true, RequeueAfter: telemetryRetryDelay}, nil
		}
		return ctrl.Result{}, errors.Wrap(err, "could not load telemetry configmap")
	}

	// Log all key/value pairs. No sorting per your request.
	if len(cm.Data) == 0 {
		reqLogger.Info("telemetry configmap has no data keys")
		return ctrl.Result{Requeue: true, RequeueAfter: telemetryRetryDelay}, nil
	}

	reqLogger.Info("start", "Telemetry configmap version", cm.GetResourceVersion())

	result, err := enterprise.ApplyTelemetry(ctx, r.Client, cm)
	if err != nil {
		reqLogger.Error(err, "Failed")
		return ctrl.Result{Requeue: true, RequeueAfter: telemetryRetryDelay}, nil
	}
	if result.Requeue && result.RequeueAfter != 0 {
		reqLogger.Info("Requeued", "period(seconds)", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *TelemetryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			labels := obj.GetLabels()
			if labels == nil {
				return false
			}
			return obj.GetName() == enterprise.GetTelemetryConfigMapName(ConfigMapNamePrefix) && labels["name"] == ConfigMapLabelName
		})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
