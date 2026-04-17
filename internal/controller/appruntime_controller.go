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
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=appruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=appruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles the AppRuntime
func (r *AppRuntimeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	metrics.ReconcileCounters.With(metrics.GetPrometheusLabels(req, "AppRuntime")).Inc()
	defer recordInstrumentionData(time.Now(), req, "controller", "AppRuntime")
	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("appruntime", req.NamespacedName)

	reqLogger.Info("entered AppRuntime reconciliation")

	// Fetch or create AppRuntime CR
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

	// Fetch or create Headless Service
	svcNN := types.NamespacedName{
		Name:      getHeadlessName(req.Name),
		Namespace: req.Namespace,
	}
	svc := &corev1.Service{}
	err = r.Get(ctx, svcNN, svc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			reqLogger.Info(svcNN.Name + " service not found; creating new one")
			svc, err = r.createHeadlessService(ctx, appRuntime, svcNN)
			if err != nil {
				reqLogger.Error(err, "failed to create service; returning reconcilation")
				return reconcile.Result{}, nil
			}
			reqLogger.Info("successfully created headless service")
		} else {
			reqLogger.Error(err, "failed to get service; returning reconciliation with error")
			return reconcile.Result{}, err
		}
	}

	// Reconcile individual Pods (one per replica, each with its own Splunk PVCs)
	parentName := getParentName(appRuntime.Name)
	parentKind := getParentKind(appRuntime.Name)
	splunkStsName := getSplunkStatefulSetName(parentName, parentKind)

	// Create missing pods
	for i := int32(0); i < appRuntime.Spec.Replicas; i++ {
		podName := getPodName(appRuntime.Name, i)
		podNN := types.NamespacedName{Name: podName, Namespace: req.Namespace}
		pod := &corev1.Pod{}
		err = r.Get(ctx, podNN, pod)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				reqLogger.Info(fmt.Sprintf("pod %s not found; creating", podName))
				err = r.createPod(ctx, appRuntime, podNN, splunkStsName, i)
				if err != nil {
					reqLogger.Error(err, fmt.Sprintf("failed to create pod %s", podName))
					return reconcile.Result{}, err
				}
				reqLogger.Info(fmt.Sprintf("created pod %s", podName))
			} else {
				reqLogger.Error(err, fmt.Sprintf("failed to get pod %s", podName))
				return reconcile.Result{}, err
			}
		}
	}

	// Delete excess pods (scale down)
	existingPods := &corev1.PodList{}
	err = r.List(ctx, existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(getCommonLabels(appRuntime.Name)),
	})
	if err != nil {
		reqLogger.Error(err, "failed to list pods")
		return reconcile.Result{}, err
	}
	for idx := range existingPods.Items {
		pod := &existingPods.Items[idx]
		ordinal, err := getPodOrdinal(pod.Name)
		if err != nil {
			continue
		}
		if ordinal >= appRuntime.Spec.Replicas {
			reqLogger.Info(fmt.Sprintf("deleting excess pod %s", pod.Name))
			if err := r.Delete(ctx, pod); err != nil {
				reqLogger.Error(err, fmt.Sprintf("failed to delete pod %s", pod.Name))
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
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
			cr.Spec.SplunkImage = enterprise.GetSplunkImage(standalone.Spec.Image)
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
			cr.Spec.SplunkImage = enterprise.GetSplunkImage(indexer.Spec.Image)
			err = ctrl.SetControllerReference(indexer, cr, r.Scheme)
			if err != nil {
				return nil, err
			}
			return cr, r.Create(ctx, cr)
		}
	case enterprise.SplunkSearchHead.ToString():
		searchHead := &enterpriseApi.SearchHeadCluster{}
		if err := r.Get(ctx, parentName, searchHead); err == nil {
			cr.Spec.Replicas = searchHead.Spec.Replicas
			cr.Spec.SplunkImage = enterprise.GetSplunkImage(searchHead.Spec.Image)
			err = ctrl.SetControllerReference(searchHead, cr, r.Scheme)
			if err != nil {
				return nil, err
			}
			return cr, r.Create(ctx, cr)
		}
	}

	return nil, nil // parent not found, nothing to create
}

func (r *AppRuntimeReconciler) createHeadlessService(ctx context.Context, ar *enterpriseApi.AppRuntime, nn types.NamespacedName) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
	}

	err := ctrl.SetControllerReference(ar, svc, r.Scheme)
	if err != nil {
		return nil, err
	}
	svc.Labels = getCommonLabels(ar.Name)
	svc.Spec.Selector = svc.Labels
	svc.Spec.ClusterIP = corev1.ClusterIPNone
	svc.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "appruntime",
			Port:       9000,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt(9000),
		},
	}
	err = r.Create(ctx, svc)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func (r *AppRuntimeReconciler) createPod(ctx context.Context, appRuntime *enterpriseApi.AppRuntime, nn types.NamespacedName, splunkStsName string, ordinal int32) error {
	splunkPodName := fmt.Sprintf("%s-%d", splunkStsName, ordinal)
	parentName := getParentName(appRuntime.Name)
	parentKind := getParentKind(appRuntime.Name)
	var instType enterprise.InstanceType
	switch parentKind {
	case enterprise.SplunkStandalone.ToString():
		instType = enterprise.SplunkStandalone
	case enterprise.SplunkIndexer.ToString():
		instType = enterprise.SplunkIndexer
	case enterprise.SplunkSearchHead.ToString():
		instType = enterprise.SplunkSearchHead
	default:
		instType = enterprise.SplunkStandalone
	}
	splunkHeadlessSvc := enterprise.GetSplunkServiceName(instType, parentName, true)
	splunkAddr := fmt.Sprintf("%s.%s.%s.svc.cluster.local", splunkPodName, splunkHeadlessSvc, nn.Namespace)

	nfsMountCmd := fmt.Sprintf(`set -e

NFS_SERVER="%s"

echo "Mounting NFS from $NFS_SERVER..."
for i in $(seq 1 30); do
  mount -t nfs4 -o soft,timeo=50,retrans=2,nolock "$NFS_SERVER":/splunk-etc /opt/splunk/etc && \
  mount -t nfs4 -o soft,timeo=50,retrans=2,nolock "$NFS_SERVER":/splunk-var /opt/splunk/var && break
  echo "NFS mount attempt $i failed, retrying in 3s..."
  umount /opt/splunk/etc 2>/dev/null || true
  sleep 3
done

if ! mountpoint -q /opt/splunk/etc; then
  echo "ERROR: failed to mount NFS /etc after 30 attempts"
  exit 1
fi
if ! mountpoint -q /opt/splunk/var; then
  echo "ERROR: failed to mount NFS /var after 30 attempts"
  exit 1
fi

echo "NFS mounts established successfully"
exec /usr/local/bin/entrypoint.sh`, splunkAddr)

	privileged := true
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels:    getCommonLabels(appRuntime.Name),
		},
		Spec: corev1.PodSpec{
			Hostname:  nn.Name,
			Subdomain: getHeadlessName(appRuntime.Name),
			InitContainers: []corev1.Container{
				{
					Name:            "copy-splunk-dirs",
					Image:           appRuntime.Spec.SplunkImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", "cp -rp /opt/splunk/lib/. /mnt/splunk-lib/ && cp -rp /opt/splunk/bin/. /mnt/splunk-bin/"},
					SecurityContext: &corev1.SecurityContext{RunAsUser: func() *int64 { uid := int64(0); return &uid }()},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "splunk-lib", MountPath: "/mnt/splunk-lib"},
						{Name: "splunk-bin", MountPath: "/mnt/splunk-bin"},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Image:           appRuntime.Spec.Image,
					Name:            "appruntime",
					ImagePullPolicy: corev1.PullAlways,
					Command:         []string{"sh", "-c", nfsMountCmd},
					Ports: []corev1.ContainerPort{
						{
							Name:          "appruntime",
							ContainerPort: 9000,
							Protocol:      corev1.ProtocolTCP,
						},
						{
							Name:          "appruntime2",
							ContainerPort: 9001,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "splunk-etc", MountPath: "/opt/splunk/etc"},
						{Name: "splunk-var", MountPath: "/opt/splunk/var"},
						{Name: "splunk-lib", MountPath: "/opt/splunk/lib"},
						{Name: "splunk-bin", MountPath: "/opt/splunk/bin"},
						{Name: "containerd-data", MountPath: "/var/lib/containerd-nested"},
						{Name: "containerd-run", MountPath: "/run/containerd-nested"},
					},
					SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name:         "splunk-etc",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
				{
					Name:         "splunk-var",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
				{
					Name:         "splunk-lib",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
				{
					Name:         "splunk-bin",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
				{
					Name:         "containerd-data",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
				{
					Name:         "containerd-run",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			},
		},
	}

	err := ctrl.SetControllerReference(appRuntime, pod, r.Scheme)
	if err != nil {
		return err
	}

	return r.Create(ctx, pod)
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
		image = "493245399694.dkr.ecr.us-west-2.amazonaws.com/appruntime/ecr-repo/supervisor:v3.1.0-appruntime"
	}
	return image
}

func (r *AppRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.AppRuntime{}).
		Watches(&enterpriseApi.Standalone{}, getEventHandlerForAppRuntime(enterprise.SplunkStandalone)).
		Watches(&enterpriseApi.IndexerCluster{}, getEventHandlerForAppRuntime(enterprise.SplunkIndexer)).
		Watches(&enterpriseApi.SearchHeadCluster{}, getEventHandlerForAppRuntime(enterprise.SplunkSearchHead)).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
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

func getCommonName(appRuntimeName string) string {
	return fmt.Sprintf("%s-%s", "splunk", appRuntimeName)
}

func getHeadlessName(appRuntimeName string) string {
	return fmt.Sprintf("%s-%s-%s", "splunk", appRuntimeName, "headless")
}

// getSplunkStatefulSetName returns the Splunk StatefulSet name: splunk-{parentName}-{parentKind}
func getSplunkStatefulSetName(parentName string, parentKind string) string {
	return fmt.Sprintf("splunk-%s-%s", parentName, parentKind)
}

// getPodName returns the AppRuntime pod name for a given ordinal: splunk-{appRuntimeName}-{ordinal}
func getPodName(appRuntimeName string, ordinal int32) string {
	return fmt.Sprintf("splunk-%s-%d", appRuntimeName, ordinal)
}

// getPodOrdinal extracts the ordinal index from a pod name (last segment after "-")
func getPodOrdinal(podName string) (int32, error) {
	parts := strings.Split(podName, "-")
	last := parts[len(parts)-1]
	var ordinal int32
	_, err := fmt.Sscanf(last, "%d", &ordinal)
	return ordinal, err
}

func getCommonLabels(appRuntimeName string) map[string]string {
	labels := make(map[string]string)
	labels["app.kubernetes.io/managed-by"] = "splunk-operator"
	labels["app.kubernetes.io/component"] = appRuntimeKindName
	labels["app.kubernetes.io/name"] = appRuntimeKindName
	labels["app.kubernetes.io/instance"] = getCommonName(appRuntimeName)
	labels["app.kubernetes.io/part-of"] = getCommonName(appRuntimeName)
	return labels
}
