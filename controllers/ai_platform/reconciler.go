package ai_platform

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/controllers/ai_platform/raybuilder"
	"github.com/splunk/splunk-operator/controllers/ai_platform/sidecars"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	//corev1 "k8s.io/api/core/v1"
)

type AIPlatformReconciler struct {
	p *enterpriseApi.SplunkAIPlatform
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func New(p *enterpriseApi.SplunkAIPlatform, client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *AIPlatformReconciler {
	return &AIPlatformReconciler{
		p:        p,
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}
}

func (r *AIPlatformReconciler) Reconcile(ctx context.Context, p *enterpriseApi.SplunkAIPlatform) (reconcile.Result, error) {

	var conditions []metav1.Condition
	defer func() {
		// Fetch the latest version of the CR before updating the status
		latest := &enterpriseApi.SplunkAIPlatform{}
		namespacedName := client.ObjectKey{Namespace: p.Namespace, Name: p.Name}
		if err := r.Get(ctx, namespacedName, latest); err != nil {
			log.FromContext(ctx).Error(err, "failed to fetch latest CR")
			return
		}
		latest.Status = p.Status
		latest.Status.Conditions = conditions
		latest.Status.ObservedGeneration = p.Generation
		_ = r.Status().Update(ctx, latest)
	}()
	raybuilder := raybuilder.New(r.p, r.Client, r.Scheme, r.Recorder)
	sidecarBuilder := sidecars.New(r.Client, r.Scheme, r.Recorder, r.p)

	stages := []struct {
		name string
		fn   func(context.Context, *enterpriseApi.SplunkAIPlatform) error
	}{
		{"Validate", r.validate},
		{"ApplicationsConfigMap", raybuilder.ReconcileApplicationsConfigMap},
		{"ServeConfigMap", raybuilder.ReconcileServeConfigMap},
		{"Sidecars", sidecarBuilder.Reconcile},
		{"rayAutoscalerRBAC", raybuilder.ReconcileRayAutoscalerRBAC},
		{"RayService", raybuilder.ReconcileRayService},
		{"WeaviateDatabase", r.ReconcileWeaviateDatabase},
		{"RayServiceStatus", raybuilder.ReconcileRayServiceStatus},
		{"WeaviateDatabaseStatus", r.ReconcileWeaviateDatabaseStatus},
	}

	for _, stage := range stages {
		err := stage.fn(ctx, p)
		cond := metav1.Condition{
			Type:               stage.name + "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "stage succeeded",
			LastTransitionTime: metav1.Now(),
		}
		if err != nil {
			cond.Status = metav1.ConditionFalse
			cond.Reason = "Error"
			cond.Message = err.Error()
			//r.Recorder.Event(p, corev1.EventTypeWarning, stage.name+"Failed", err.Error())
		} else {
			//r.Recorder.Event(p, corev1.EventTypeNormal, stage.name+"Succeeded", "stage succeeded")
		}
		conditions = append(conditions, cond)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// all done
	conditions = append(conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "AllReconciled",
		Message:            "all stages completed successfully",
		LastTransitionTime: metav1.Now(),
	})

	return reconcile.Result{}, nil
}
