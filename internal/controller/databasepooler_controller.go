package controller

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/database/provider/cnpg"
)

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databasepoolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databasepoolers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups;scheduledbackups;clusters;poolers,verbs=get;list;watch;create;update;patch;delete

type DatabasePoolerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DatabasePoolerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var dp v4.DatabasePooler
	if err := r.Get(ctx, req.NamespacedName, &dp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	svc, ready, err := (&cnpg.CNPGProvider{Client: r.Client}).EnsurePooler(ctx, &dp)
	if err != nil {
		upsertCond(&dp.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "Error",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, &dp)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	dp.Status.ServiceName = svc
	upsertCond(&dp.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus(ready),
		Reason:             "Observed",
		Message:            "Pooler ensured",
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, &dp)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *DatabasePoolerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		For(&v4.DatabasePooler{}).
		Complete(r)
}
