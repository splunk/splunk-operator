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

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databasebackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databasebackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=databaseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups;scheduledbackups;clusters;poolers,verbs=get;list;watch;create;update;patch;delete

type DatabaseBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DatabaseBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var dbbk v4.DatabaseBackup
	if err := r.Get(ctx, req.NamespacedName, &dbbk); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch target DatabaseCluster
	var target v4.DatabaseCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: dbbk.Spec.TargetRef.Name}, &target); err != nil {
		setBkpStatus(&dbbk, v4.BackupFailed, "TargetNotFound", err.Error())
		_ = r.Status().Update(ctx, &dbbk)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Trigger CNPG Backup (idempotent upsert)
	prov := &cnpg.CNPGProvider{Client: r.Client}
	name, err := prov.TriggerAdhocBackup(ctx, &dbbk, &target)
	if err != nil {
		setBkpStatus(&dbbk, v4.BackupFailed, "CreateError", err.Error())
		_ = r.Status().Update(ctx, &dbbk)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Inspect CNPG Backup status
	phase, bid, completed, msg, ierr := prov.InspectBackup(ctx, req.Namespace, name)
	if ierr == nil {
		dbbk.Status.CNPGBackupRef = &v4.LocalRef{Name: name}
		dbbk.Status.BackupID = bid
		dbbk.Status.CompletedAt = completed
		switch phase {
		case "completed", "succeeded", "complete", "success", "ok":
			setBkpStatus(&dbbk, v4.BackupSucceeded, "Completed", "Backup completed")
		case "running", "started", "in_progress", "in-progress":
			setBkpStatus(&dbbk, v4.BackupRunning, "Running", "Backup running")
		case "failed", "error":
			setBkpStatus(&dbbk, v4.BackupFailed, "Failed", msg)
		default:
			setBkpStatus(&dbbk, v4.BackupPending, "Pending", "Backup pending")
		}
	} else {
		setBkpStatus(&dbbk, v4.BackupPending, "Waiting", "Waiting for CNPG Backup")
	}

	_ = r.Status().Update(ctx, &dbbk)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *DatabaseBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		For(&v4.DatabaseBackup{}).
		Complete(r)
}

func setBkpStatus(b *v4.DatabaseBackup, phase v4.BackupPhase, reason, msg string) {
	b.Status.Phase = phase
	upsertCond(&b.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus(phase == v4.BackupSucceeded),
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

func upsertCond(list *[]metav1.Condition, c metav1.Condition) {
	i := -1
	for idx := range *list {
		if (*list)[idx].Type == c.Type {
			i = idx
			break
		}
	}
	if i >= 0 {
		(*list)[i] = c
	} else {
		*list = append(*list, c)
	}
}
func condStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}
