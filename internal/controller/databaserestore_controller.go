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

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaserestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaserestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters;backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups;scheduledbackups;clusters;poolers,verbs=get;list;watch;create;update;patch;delete

type DatabaseRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DatabaseRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var dr v4.DatabaseRestore
	if err := r.Get(ctx, req.NamespacedName, &dr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate mode
	if dr.Spec.Mode == v4.RestoreInPlace {
		setRestore(&dr, v4.RestoreFailed, "Unsupported", "InPlace restore is not supported; use mode=NewCluster")
		_ = r.Status().Update(ctx, &dr)
		return ctrl.Result{}, nil
	}

	// Get source cluster (for defaults)
	var src v4.DatabaseCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: dr.Spec.SourceClusterRef.Name}, &src); err != nil {
		setRestore(&dr, v4.RestoreFailed, "SourceNotFound", err.Error())
		_ = r.Status().Update(ctx, &dr)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Create recovered CNPG Cluster
	prov := &cnpg.CNPGProvider{Client: r.Client}
	if err := prov.RestoreNewClusterFromBackup(ctx, &src, &dr); err != nil {
		setRestore(&dr, v4.RestoreFailed, "CreateError", err.Error())
		_ = r.Status().Update(ctx, &dr)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Surface restored ref and Running phase; readiness can be checked later
	name := dr.Spec.NewCluster.Name
	if name == "" {
		name = dr.Name + "-restored"
	}
	dr.Status.RestoredRef = &v4.LocalRef{Name: name}
	setRestore(&dr, v4.RestoreRunning, "Creating", "Recovery cluster being created")
	_ = r.Status().Update(ctx, &dr)

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *DatabaseRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		For(&v4.DatabaseRestore{}).
		Complete(r)
}

func setRestore(dr *v4.DatabaseRestore, phase v4.RestorePhase, reason, msg string) {
	dr.Status.Phase = phase
	now := metav1.Now()
	if dr.Status.StartedAt == nil {
		dr.Status.StartedAt = &now
	}
	if phase == v4.RestoreCompleted || phase == v4.RestoreFailed {
		t := metav1.Now()
		dr.Status.CompletedAt = &t
	}
	upsertCond(&dr.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus(phase == v4.RestoreCompleted),
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}
