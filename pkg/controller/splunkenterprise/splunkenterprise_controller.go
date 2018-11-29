package splunkenterprise

import (
	"context"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"log"

	enterprisev1alpha1 "git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new SplunkEnterprise Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileSplunkEnterprise{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("splunkenterprise-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource SplunkEnterprise
	err = c.Watch(&source.Kind{Type: &enterprisev1alpha1.SplunkEnterprise{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner SplunkEnterprise
	//err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	//	IsController: true,
	//	OwnerType:    &enterprisev1alpha1.SplunkEnterprise{},
	//})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileSplunkEnterprise{}

// ReconcileSplunkEnterprise reconciles a SplunkEnterprise object
type ReconcileSplunkEnterprise struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a SplunkEnterprise object and makes changes based on the state read
// and what is in the SplunkEnterprise.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSplunkEnterprise) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("Reconciling SplunkEnterprise %s/%s\n", request.Namespace, request.Name)

	// Fetch the SplunkEnterprise instance
	instance := &enterprisev1alpha1.SplunkEnterprise{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = enterprise.ValidateSplunkCustomResource(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = enterprise.LaunchDeployment(instance, r.client)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
