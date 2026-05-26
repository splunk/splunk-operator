package controller

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	sharedAppRuntimeLabelKey   = "enterprise.splunk.com/shared-app-runtime"
	sharedAppRuntimeNodeLabel  = "enterprise.splunk.com/shared-app-runtime-node"
	sharedAppRuntimeAppLabel   = "enterprise.splunk.com/shared-app-runtime-app"
	sharedAppRuntimePodLabel   = "enterprise.splunk.com/shared-app-runtime-pod"
	splunkPodLabelKey          = "app.kubernetes.io/name"
	sharedAppRuntimeDataVolume = "shared-data"
	dispatcherDiscoveryPort    = 9000
	dispatcherProxyPort        = 9001

	// appDiscoveryCMPrefix mirrors the prefix the sidecar writes, keyed by
	// Splunk pod name. The controller lists all ConfigMaps with the
	// sharedAppRuntimePodLabel set and unions their "apps" lines to decide
	// which (node, app) pods to reconcile.
	appDiscoveryCMPrefix = "appruntime-apps-"
	appDiscoveryDataKey  = "apps"

	dnsLabelMaxLen = 63
)

type SharedAppRuntimeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=sharedappruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=sharedappruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *SharedAppRuntimeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("sharedappruntime", req.NamespacedName)

	sar := &enterpriseApi.SharedAppRuntime{}
	if err := r.Get(ctx, req.NamespacedName, sar); err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	splunkPods, err := r.listSplunkPods(ctx, req.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	nodeToSplunkPods := groupSplunkPodsByNode(splunkPods)

	// Union apps declared in the CR with apps discovered via the sidecar
	// ConfigMaps. Spec.Apps stays authoritative for PoC bootstrapping, but
	// anything the sidecar reports on a Splunk pod also reconciles.
	discoveredApps, err := r.listDiscoveredApps(ctx, req.Namespace)
	if err != nil {
		logger.Error(err, "failed to list app-discovery configmaps")
		return reconcile.Result{}, err
	}

	desired := map[string]desiredAppPod{}
	for node, pods := range nodeToSplunkPods {
		appSet := map[string]struct{}{}
		for _, a := range sar.Spec.Apps {
			appSet[a] = struct{}{}
		}
		for _, p := range pods {
			for _, a := range discoveredApps[p.Name] {
				appSet[a] = struct{}{}
			}
		}
		for app := range appSet {
			name := sharedAppPodName(node, app)
			desired[name] = desiredAppPod{
				node: node,
				app:  app,
				pods: pods,
			}
		}
	}

	existing, err := r.listOwnedAppPods(ctx, sar)
	if err != nil {
		return reconcile.Result{}, err
	}

	for name, d := range desired {
		if err := r.ensureService(ctx, sar, name, d); err != nil {
			logger.Error(err, "failed to ensure service", "name", name)
			return reconcile.Result{}, err
		}
		if existingPod, ok := existing[name]; ok {
			if podNeedsRecreation(existingPod, d) {
				logger.Info("recreating app pod due to topology change", "name", name)
				if err := r.Delete(ctx, existingPod); err != nil && !k8serrors.IsNotFound(err) {
					return reconcile.Result{}, err
				}
				continue
			}
		} else {
			if err := r.createAppPod(ctx, sar, name, d); err != nil {
				logger.Error(err, "failed to create app pod", "name", name)
				return reconcile.Result{}, err
			}
		}
	}

	for name, pod := range existing {
		if _, ok := desired[name]; !ok {
			logger.Info("deleting stale app pod", "name", name)
			if err := r.Delete(ctx, pod); err != nil && !k8serrors.IsNotFound(err) {
				return reconcile.Result{}, err
			}
		}
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	sar.Status.Phase = enterpriseApi.PhaseReady
	sar.Status.ReconciledPods = names
	sar.Status.Message = fmt.Sprintf("%d app pods across %d nodes", len(names), len(nodeToSplunkPods))
	if err := r.Status().Update(ctx, sar); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

type desiredAppPod struct {
	node string
	app  string
	pods []corev1.Pod
}

func (r *SharedAppRuntimeReconciler) listSplunkPods(ctx context.Context, ns string) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	out := []corev1.Pod{}
	for _, p := range list.Items {
		if _, ok := p.Labels["app.kubernetes.io/managed-by"]; !ok {
			continue
		}
		if p.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
			continue
		}
		if p.Spec.NodeName == "" {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// listDiscoveredApps returns a map of Splunk pod name -> app list, built
// from the app-discovery ConfigMaps written by the per-pod sidecar. Only
// ConfigMaps carrying sharedAppRuntimePodLabel are considered, and the
// label value keys the result (not the ConfigMap name).
func (r *SharedAppRuntimeReconciler) listDiscoveredApps(ctx context.Context, ns string) (map[string][]string, error) {
	list := &corev1.ConfigMapList{}
	if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, cm := range list.Items {
		pod := cm.Labels[sharedAppRuntimePodLabel]
		if pod == "" {
			if !strings.HasPrefix(cm.Name, appDiscoveryCMPrefix) {
				continue
			}
			pod = strings.TrimPrefix(cm.Name, appDiscoveryCMPrefix)
		}
		raw := cm.Data[appDiscoveryDataKey]
		if raw == "" {
			continue
		}
		apps := []string{}
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			apps = append(apps, line)
		}
		if len(apps) > 0 {
			out[pod] = apps
		}
	}
	return out, nil
}

func groupSplunkPodsByNode(pods []corev1.Pod) map[string][]corev1.Pod {
	out := map[string][]corev1.Pod{}
	for _, p := range pods {
		out[p.Spec.NodeName] = append(out[p.Spec.NodeName], p)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].Name < out[k][j].Name })
	}
	return out
}

func (r *SharedAppRuntimeReconciler) listOwnedAppPods(ctx context.Context, sar *enterpriseApi.SharedAppRuntime) (map[string]*corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := r.List(ctx, list, client.InNamespace(sar.Namespace), client.MatchingLabels{sharedAppRuntimeLabelKey: sar.Name}); err != nil {
		return nil, err
	}
	out := map[string]*corev1.Pod{}
	for i := range list.Items {
		p := &list.Items[i]
		out[p.Name] = p
	}
	return out, nil
}

// sharedAppPodVolumeTypes lists the per-instance PVC suffixes the dispatcher
// mounts for each co-located Splunk pod. Matches the SOK-side PVC naming
// (splcommon.PvcNamePrefix = "pvc-<type>") so e.g. volumeType "etc" + pod name
// "splunk-s1-standalone-0" produces claim "pvc-etc-splunk-s1-standalone-0".
var sharedAppPodVolumeTypes = []string{"etc", "var", "bin", "lib"}

func claimName(volumeType, podName string) string {
	return "pvc-" + volumeType + "-" + podName
}

// podNeedsRecreation returns true if the set of co-located Splunk pods on this node
// has changed since the app pod was created. Q20: any change triggers recreation.
func podNeedsRecreation(existing *corev1.Pod, d desiredAppPod) bool {
	have := map[string]bool{}
	for _, vol := range existing.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			have[vol.PersistentVolumeClaim.ClaimName] = true
		}
	}
	want := map[string]bool{}
	for _, p := range d.pods {
		for _, vt := range sharedAppPodVolumeTypes {
			want[claimName(vt, p.Name)] = true
		}
	}
	if len(have) == 0 && len(want) == 0 {
		return false
	}
	if len(have) < len(want) {
		return true
	}
	for k := range want {
		if !have[k] {
			return true
		}
	}
	extra := 0
	for k := range have {
		if !want[k] {
			extra++
		}
	}
	return extra > 0
}

func (r *SharedAppRuntimeReconciler) ensureService(ctx context.Context, sar *enterpriseApi.SharedAppRuntime, name string, d desiredAppPod) error {
	svc := &corev1.Service{}
	nn := types.NamespacedName{Namespace: sar.Namespace, Name: name}
	err := r.Get(ctx, nn, svc)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}
	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sar.Namespace,
			Labels: map[string]string{
				sharedAppRuntimeLabelKey:  sar.Name,
				sharedAppRuntimeNodeLabel: sanitizeLabel(d.node),
				sharedAppRuntimeAppLabel:  sanitizeLabel(d.app),
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(sar, sar.GroupVersionKind())},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				sharedAppRuntimeLabelKey: sar.Name,
				"pod-name":               name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "discovery",
					Port:       dispatcherDiscoveryPort,
					TargetPort: intstr.FromInt(dispatcherDiscoveryPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "proxy",
					Port:       dispatcherProxyPort,
					TargetPort: intstr.FromInt(dispatcherProxyPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			PublishNotReadyAddresses: true,
		},
	}
	return r.Create(ctx, svc)
}

func (r *SharedAppRuntimeReconciler) createAppPod(ctx context.Context, sar *enterpriseApi.SharedAppRuntime, name string, d desiredAppPod) error {
	// Per-instance bin/lib PVCs (provisioned by SOK's populate-shared-bin-lib
	// init container on each Splunk pod) replace the former EmptyDir + copy
	// init container: the dispatcher mounts each Splunk instance's own
	// {etc,var,bin,lib} claims at /data/<pod>/<type>.
	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount
	for _, p := range d.pods {
		for _, vt := range sharedAppPodVolumeTypes {
			volName := vt + "-" + p.Name
			volumes = append(volumes, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName(vt, p.Name)},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{
				Name:      volName,
				MountPath: "/data/" + p.Name + "/" + vt,
			})
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sar.Namespace,
			Labels: map[string]string{
				sharedAppRuntimeLabelKey:  sar.Name,
				sharedAppRuntimeNodeLabel: sanitizeLabel(d.node),
				sharedAppRuntimeAppLabel:  sanitizeLabel(d.app),
				"pod-name":                name,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(sar, sar.GroupVersionKind())},
		},
		Spec: corev1.PodSpec{
			NodeName:      d.node,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  "dispatcher",
				Image: sar.Spec.AppPodImage,
				Env: []corev1.EnvVar{
					{Name: "APPRUNTIME_APP_NAME", Value: d.app},
					{Name: "APPRUNTIME_NODE_NAME", Value: d.node},
					{Name: "APPRUNTIME_SHARED_ENABLED", Value: "true"},
					{Name: "APPRUNTIME_NSJAIL_ENABLED", Value: "true"},
					{Name: "APPRUNTIME_DATA_ROOT", Value: "/data"},
					// Address the supervisor advertises to remote shims as the
					// execution proxy host. Must match this Service's FQDN so
					// the shim can dial it; the supervisor itself binds 0.0.0.0.
					{Name: "APPRUNTIME_DISPATCHER_HOST", Value: fmt.Sprintf("%s.%s.svc.cluster.local", name, sar.Namespace)},
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "discovery",
						ContainerPort: dispatcherDiscoveryPort,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "proxy",
						ContainerPort: dispatcherProxyPort,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				VolumeMounts: mounts,
			}},
			Volumes: volumes,
		},
	}
	return r.Create(ctx, pod)
}

// sharedAppPodName builds the (node, app) pod name, matching the Service FQDN
// used by the shim. Format: appruntime-<nodeId>-<appId>.
func sharedAppPodName(node, app string) string {
	prefix := "appruntime-"
	nodeId := sanitizeLabel(node)
	appId := sanitizeLabel(app)
	name := prefix + nodeId + "-" + appId
	if len(name) > dnsLabelMaxLen {
		trim := len(name) - dnsLabelMaxLen
		if len(nodeId) > trim+8 {
			nodeId = nodeId[:len(nodeId)-trim]
			name = prefix + nodeId + "-" + appId
		}
		if len(name) > dnsLabelMaxLen {
			name = name[:dnsLabelMaxLen]
		}
	}
	return strings.TrimRight(name, "-")
}

var nonDNSLabel = regexp.MustCompile(`[^a-z0-9-]`)

func sanitizeLabel(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = nonDNSLabel.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func (r *SharedAppRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("sharedappruntime").
		For(&enterpriseApi.SharedAppRuntime{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.splunkPodToSAR),
			builder.WithPredicates(),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.appDiscoveryCMToSAR),
			builder.WithPredicates(),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

func (r *SharedAppRuntimeReconciler) appDiscoveryCMToSAR(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	if cm.Labels[sharedAppRuntimePodLabel] == "" && !strings.HasPrefix(cm.Name, appDiscoveryCMPrefix) {
		return nil
	}
	list := &enterpriseApi.SharedAppRuntimeList{}
	if err := r.List(ctx, list, client.InNamespace(cm.Namespace)); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, sar := range list.Items {
		out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Name: sar.Name, Namespace: sar.Namespace}})
	}
	return out
}

func (r *SharedAppRuntimeReconciler) splunkPodToSAR(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	if pod.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
		return nil
	}
	list := &enterpriseApi.SharedAppRuntimeList{}
	if err := r.List(ctx, list, client.InNamespace(pod.Namespace)); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, sar := range list.Items {
		out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Name: sar.Name, Namespace: sar.Namespace}})
	}
	return out
}
