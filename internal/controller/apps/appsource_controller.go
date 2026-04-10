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

package apps

import (
	"context"
	"time"

	corev1
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
)

// AppSourceReconciler reconciles a AppSource object
type AppSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.splunk.com,resources=appsources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppSource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *AppSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	appSourceInstance := &appsv1alpha1.AppSource{}

	if err := r.Get(ctx, req.NamespacedName, appSourceInstance); err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			logger.Info("AppSource resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get AppSource")
		return ctrl.Result{}, err
	}

	// initialize conditions if needed
	if len(appSourceInstance.Status.Conditions) == 0 {
		meta.SetStatusCondition(&appSourceInstance.Status.Conditions, metav1.Condition{
			Type:    appsv1alpha1.TypeAppSourceConditionPending,
			Status:  metav1.ConditionTrue,
			Reason:  "AppSourceInitialized",
			Message: "AppSource resource has been initialized",
		})

		if err := r.Status().Update(ctx, appSourceInstance); err != nil {
			logger.Error(err, "Failed to update AppSource conditions")
			return ctrl.Result{}, err
		}

		// Requeue to process the AppSource after conditions are initialized
		return ctrl.Result{Requeue: true}, nil
	}

	// check if we need to reconcile based on the observed generation changes or the periodic poll
	needsToReconcile := appSourceInstance.Generation != appSourceInstance.Status.ObservedGeneration
	// if the generation changed, we need to reconcile asap; we shouldnt enter this block
	// if the generation has not changed, check if we need to reconcile based on the periodic poll
	if !needsToReconcile && appSourceInstance.Status.LastPolledTime != nil {
		// calculate the next poll time; we get the last sync time and add the poll interval
		nextPoll := appSourceInstance.Status.LastPolledTime.Add(time.Duration(*appSourceInstance.Spec.PollIntervalSeconds) * time.Second)
		// check if the current time is earlier than polling time
		if time.Now().Before(nextPoll) {
			return ctrl.Result{RequeueAfter: time.Until(nextPoll)}, nil
		}
	}

	// at this point we know it needs to reconcile
	logger.Info("Reconciling AppSource", "namespacedName", req.NamespacedName, "name", req.Name, "secretName", appSourceInstance.Spec.Auth.SecretRef.Name)
	secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      appSourceInstance.Spec.Auth.SecretRef.Name,
			Namespace: appSourceInstance.Namespace,
		}
		if err := r.Get(ctx, secretKey, secret); err != nil {
			logger.Error(err, "Failed to get secret")
			return ctrl.Result{}, err
		}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppSource{}).
		Named("apps-appsource").
		Complete(r)
}
