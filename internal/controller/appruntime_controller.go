package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AppRuntimeReconciler reconsiles a AppRuntime object
type AppRuntimeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=appruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=appruntimes/status,verbs=get;update;patch

// Reconcile reconciles the AppRuntime
func (r *AppRuntimeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "AppRuntime")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "AppRuntime")
	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("appruntime", req.NamespacedName)

	reqLogger.Info("entered AppRuntime reconciliation")

	// Fetch or create the AppRuntime
	appRuntime := &enterpriseApi.AppRuntime{}
	err := r.Get(ctx, req.NamespacedName, appRuntime)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			reqLogger.Info(req.Name + " appruntime not found; create new one")
			appRuntime, err = r.createCR(ctx, req.NamespacedName)
			if err != nil {
				reqLogger.Error(err, "failed to create appruntime; returning reconcilation")
				return reconcile.Result{}, err
			}
			if appRuntime == nil {
				// Parent was deleted, nothing to do
				reqLogger.Info("appruntime is nil - the parent was deleted; returning reconcilation")
				return reconcile.Result{}, nil
			}
			reqLogger.Info(fmt.Sprintf("created %s successfully", appRuntime.Name))
		}
	}
	appRuntime, err = r.checkReplicas(ctx, req, appRuntime, reqLogger, err)
	if err != nil {
		reqLogger.Error(err, "failed to update replicas")
		return reconcile.Result{}, err
	}

	// Fetch or create StatefulSet
	statefulSetNN := types.NamespacedName{
		Name:      getStatefulSetName(req.Name),
		Namespace: req.Namespace,
	}
	statefulSet := &appsv1.StatefulSet{}
	err = r.Get(ctx, statefulSetNN, statefulSet)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			reqLogger.Info(statefulSetNN.Name + " statefulSet not found; creating new one")
			statefulSet, err = r.createStatefulSet(ctx, appRuntime, statefulSetNN)
			if err != nil {
				reqLogger.Error(err, "failed to create statefulset; returning reconcilation")
				return reconcile.Result{}, nil
			}
		}
		reqLogger.Error(err, "failed to get statefulset; returning reconciliation with error")
		return reconcile.Result{}, err
	}
	// Check statefulSet Replicas
	err = r.checkStatefulSetReplicas(ctx, statefulSet, appRuntime, err, reqLogger)
	if err != nil {
		reqLogger.Error(err, "failed to check statefulset replicas")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *AppRuntimeReconciler) checkStatefulSetReplicas(ctx context.Context, statefulSet *appsv1.StatefulSet, appRuntime *enterpriseApi.AppRuntime, err error, reqLogger logr.Logger) error {
	if *statefulSet.Spec.Replicas != appRuntime.Spec.Replicas {
		statefulSet.Spec.Replicas = &appRuntime.Spec.Replicas
		err = r.Update(ctx, statefulSet)
		if err != nil {
			reqLogger.Error(err, "cannot update statefulset")
			return err
		}
		reqLogger.Info("updated statefulset replicas")
	}
	return nil
}

// checkReplicas check if replicas number is correct
func (r *AppRuntimeReconciler) checkReplicas(ctx context.Context, req reconcile.Request, appRuntime *enterpriseApi.AppRuntime, reqLogger logr.Logger, err error) (*enterpriseApi.AppRuntime, error) {
	var parentReplicas int32
	switch getParentKind(appRuntime.GetName()) { // todo mb: merge this with the code in createCR
	case enterprise.SplunkStandalone.ToString():
		standalone := &enterpriseApi.Standalone{}
		if err := r.Get(ctx, types.NamespacedName{Name: getParentName(appRuntime.Name), Namespace: req.Namespace}, standalone); err == nil {
			reqLogger.Info(fmt.Sprintf("parent: %v", standalone))
			parentReplicas = standalone.Spec.Replicas
		} else {
			reqLogger.Error(err, "cannot get parent")
			return nil, err
		}
	case enterprise.SplunkIndexer.ToString():
		indexer := &enterpriseApi.IndexerCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: getParentName(appRuntime.Name), Namespace: req.Namespace}, indexer); err == nil {
			reqLogger.Info(fmt.Sprintf("parent: %v", indexer))
			parentReplicas = indexer.Spec.Replicas
		} else {
			reqLogger.Error(err, "cannot get parent")
			return nil, err

		}
	case enterprise.SplunkSearchHead.ToString():
		shc := &enterpriseApi.SearchHeadCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: getParentName(appRuntime.Name), Namespace: req.Namespace}, shc); err == nil {
			reqLogger.Info(fmt.Sprintf("parent: %v", shc))
			parentReplicas = shc.Spec.Replicas
		} else {
			reqLogger.Error(err, "cannot get parent")
			return nil, err

		}
	}
	if parentReplicas == 0 {
		parentReplicas = 1 // Because the parent actually runs 1 pod because the Standalone controller defaults unset replicas to 1 internally — but the Spec.Replicas field itself stays 0.
	}
	if parentReplicas != appRuntime.Spec.Replicas {
		reqLogger.Info("needs to update replicas number")
		appRuntime.Spec.Replicas = parentReplicas
		err = r.Update(ctx, appRuntime)
		if err != nil {
			reqLogger.Error(err, "cannot update appruntime")
			return nil, err
		}
		reqLogger.Info("updated replicas number")
		return appRuntime, nil
	}

	reqLogger.Info(fmt.Sprintf("did not update replicas number - appruntime:%d, parent:%d", appRuntime.Spec.Replicas, parentReplicas))
	return appRuntime, nil
}

func (r *AppRuntimeReconciler) createCR(ctx context.Context, crNN types.NamespacedName) (*enterpriseApi.AppRuntime, error) {
	parentName := types.NamespacedName{
		Name:      getParentName(crNN.Name),
		Namespace: crNN.Namespace,
	}

	cr := &enterpriseApi.AppRuntime{
		ObjectMeta: v1.ObjectMeta{
			Name:      crNN.Name,
			Namespace: crNN.Namespace,
		},
		Spec: enterpriseApi.AppRuntimeSpec{
			Image: getImageFromEnv(),
		},
	}

	// Find the parent and set the reference
	switch getParentKind(cr.GetName()) {
	case enterprise.SplunkStandalone.ToString():
		standalone := &enterpriseApi.Standalone{}
		if err := r.Get(ctx, parentName, standalone); err == nil {
			cr.Spec.Replicas = standalone.Spec.Replicas
			err = ctrl.SetControllerReference(standalone, cr, r.Scheme)
			if err != nil {
				return nil, err
			}
			return cr, r.Create(ctx, cr)
		}
	case enterprise.SplunkIndexer.ToString():
		indexer := &enterpriseApi.IndexerCluster{}
		if err := r.Get(ctx, parentName, indexer); err == nil {
			cr.Spec.Replicas = indexer.Spec.Replicas
			err = ctrl.SetControllerReference(indexer, cr, r.Scheme)
			if err != nil {
				return nil, err
			}
			return cr, r.Create(ctx, cr)
		}
	case enterprise.SplunkSearchHead.ToString():
		searchHead := &enterpriseApi.SearchHeadCluster{} // can it be client.Object and then reuse the following code?
		if err := r.Get(ctx, parentName, searchHead); err == nil {
			cr.Spec.Replicas = searchHead.Spec.Replicas
			err = ctrl.SetControllerReference(searchHead, cr, r.Scheme)
			if err != nil {
				return nil, err
			}
			return cr, r.Create(ctx, cr)
		}
	}

	return nil, nil // parent not found, nothing to create
}

func (r *AppRuntimeReconciler) createStatefulSet(ctx context.Context, appRuntime *enterpriseApi.AppRuntime, nn types.NamespacedName) (*appsv1.StatefulSet, error) {
	ss := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
	}
	err := ctrl.SetControllerReference(appRuntime, ss, r.Scheme)
	if err != nil {
		return nil, err
	}
	ss.Spec.Replicas = &appRuntime.Spec.Replicas
	ss.Labels = make(map[string]string)
	ss.Labels["app.kubernetes.io/managed-by"] = "splunk-operator"
	ss.Labels["app.kubernetes.io/component"] = appRuntimeKindName
	ss.Labels["app.kubernetes.io/name"] = appRuntimeKindName
	ss.Labels["app.kubernetes.io/instance"] = nn.Name
	ss.Labels["app.kubernetes.io/part-of"] = nn.Name
	ss.Spec.Selector = &v1.LabelSelector{MatchLabels: ss.Labels}
	ss.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
	ss.Spec.Template = corev1.PodTemplateSpec{
		ObjectMeta: v1.ObjectMeta{
			Labels: ss.Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: appRuntime.Spec.Image,
					Name:  "appruntime",
					Command: []string{
						"sleep",
						"infinity",
					},
				},
			},
		},
	}
	err = r.Create(ctx, ss)
	if err != nil {
		return nil, err
	}

	return &appsv1.StatefulSet{}, nil
}

func (r *AppRuntimeReconciler) updateStatus(ctx context.Context, appRuntime *enterpriseApi.AppRuntime, phase enterpriseApi.Phase, message string) error {
	appRuntime.Status.Phase = phase
	appRuntime.Status.Message = message
	if err := r.Status().Update(ctx, appRuntime); err != nil {
		return errors.Wrap(err, "failed to update appruntime status")
	}
	return nil
}

func getImageFromEnv() string {
	image, ok := os.LookupEnv("RELATED_IMAGE_APP_RUNTIME")
	if !ok {
		image = "busybox:latest"
	}
	return image
}

func (r *AppRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.AppRuntime{}).
		Watches(&enterpriseApi.Standalone{}, getEventHandlerForAppRuntime(enterprise.SplunkStandalone)).
		Watches(&enterpriseApi.IndexerCluster{}, getEventHandlerForAppRuntime(enterprise.SplunkIndexer)).
		Watches(&enterpriseApi.SearchHeadCluster{}, getEventHandlerForAppRuntime(enterprise.SplunkSearchHead)).
		Owns(&appsv1.StatefulSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: enterpriseApi.TotalWorker}).
		Named("appruntime-controller").
		Complete(r)
}

func getEventHandlerForAppRuntime(parentType enterprise.InstanceType) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      getAppRuntimeName(obj.GetName(), parentType.ToString()),
					Namespace: obj.GetNamespace(),
				},
			}}
		},
	)
}

const appRuntimeKindName = "appruntime"

func getAppRuntimeName(parentName string, parentType string) string {
	return fmt.Sprintf("%s-%s-%s", parentName, parentType, appRuntimeKindName)
}

func getParentName(appRuntimeName string) string {
	return strings.Split(appRuntimeName, "-")[0] // todo mb: bug - if name consists '-'
}

func getParentKind(appRuntimeName string) string {
	return strings.Split(appRuntimeName, "-")[1] // todo mb: bug - if name consists '-'
}

func getStatefulSetName(appRuntimeName string) string {
	return fmt.Sprintf("%s-%s", "splunk", appRuntimeName)
}
