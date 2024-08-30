/*
Copyright 2021.

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

package enterprise

import (
	"context"

	//promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	genai "github.com/splunk/splunk-operator/pkg/splunk/genai"
	//"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GenAIDeploymentReconciler reconciles a GenAIDeployment object
type GenAIDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// ApplyGenAIDeployment reconciles the state of a Splunk Enterprise cluster manager.
func ApplyGenAIDeployment(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.GenAIDeployment) (reconcile.Result, error) {
	g := &GenAIDeploymentReconciler{Client: client, Scheme: client.Scheme()}
	return g.Reconcile(ctx, cr)
}

// Reconcile is the main reconciliation loop for GenAIDeployment.
func (r *GenAIDeploymentReconciler) Reconcile(ctx context.Context, cr *enterpriseApi.GenAIDeployment) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Initialize reconcilers for each service
	saisReconciler := genai.NewSaisServiceReconciler(r.Client, cr)
	vectorDbReconciler := genai.NewVectorDbReconciler(r.Client, cr)
	rayReconciler := genai.NewRayServiceReconciler(r.Client, cr)

	// Reconcile SaisService
	saisStatus, err := saisReconciler.Reconcile(ctx)
	if err != nil {
		log.Error(err, "Failed to reconcile SaisService")
		return ctrl.Result{}, err
	}
	cr.Status.SaisServiceStatus = saisStatus

	// Reconcile VectorDbService
	vectorDbStatus, err := vectorDbReconciler.Reconcile(ctx)
	if err != nil {
		log.Error(err, "Failed to reconcile VectorDbService")
		return ctrl.Result{}, err
	}
	cr.Status.VectorDbStatus = vectorDbStatus

	// Reconcile RayService
	rayStatus, err := rayReconciler.Reconcile(ctx)
	if err != nil {
		log.Error(err, "Failed to reconcile RayService")
		return ctrl.Result{}, err
	}
	cr.Status.RayClusterStatus = rayStatus

	// Update the status of GenAIDeployment in Kubernetes
	if err := r.Status().Update(ctx, cr); err != nil {
		log.Error(err, "Failed to update GenAIDeployment status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled GenAIDeployment", "GenAIDeployment.Namespace", cr.Namespace, "GenAIDeployment.Name", cr.Name)
	return ctrl.Result{}, nil
}
