/*
File: controllers/raybuilder/builder.go
*/
package raybuilder

import (
	"context"
	"fmt"
	"os"
	"strings"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/controllers/ai_platform/sidecars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	//"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Builder encapsulates RayService generation logic.
type Builder struct {
	ai *enterpriseApi.AIPlatform
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// New returns a new Builder for the given AIPlatform instance.
func New(ai *enterpriseApi.AIPlatform, client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *Builder {
	return &Builder{
		ai:       ai,
		Client:   client,
		Scheme:   scheme,
		Recorder: recorder,
	}
}

// --- 7️⃣ ReconcileRayService: build & create/update the RayService CR ---
func (b *Builder) ReconcileRayService(ctx context.Context, p *enterpriseApi.AIPlatform) error {
	logger := log.FromContext(ctx) // Define logger
	rs := b.Build()

	// Fetch the ServeConfigMap
	serveConfigMap := &corev1.ConfigMap{}
	serveConfigMapKey := types.NamespacedName{Namespace: p.Namespace, Name: p.Name + "-serveconfig"}
	if err := b.Client.Get(ctx, serveConfigMapKey, serveConfigMap); err != nil {
		return err
	}

	annotations, labels := buildHeadAnnotationsAndLabels(p)
	rayService := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
		},
	}
	err := b.Client.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: p.Name}, rayService)
	if errors.IsNotFound(err) {
		rayService = &rayv1.RayService{
			ObjectMeta: metav1.ObjectMeta{
				Name:        p.Name,
				Namespace:   p.Namespace,
				Annotations: annotations,
				Labels:      labels,
			},
		}
	}

	// Add ServeConfigMap to RayService annotations FIXME
	if serveConfig, exists := serveConfigMap.Data["serveconfig.yaml"]; exists {
		rs.Spec.ServeConfigV2 = serveConfig
	} else {
		logger.Error(fmt.Errorf("serveconfig.yaml not found"), "ServeConfigMap is missing serveconfig.yaml key")
		return fmt.Errorf("serveconfig.yaml not found in ConfigMap %s", serveConfigMapKey.Name)
	}

	rayService.Spec = rs.Spec
	key := types.NamespacedName{Namespace: rayService.Namespace, Name: rayService.Name}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current rayv1.RayService
		if err := b.Client.Get(ctx, key, &current); err != nil {
			if errors.IsNotFound(err) {
				controllerutil.SetOwnerReference(p, rayService, b.Scheme)
				return b.Client.Create(ctx, rayService)
			}
			b.Recorder.Eventf(p, corev1.EventTypeWarning, "ReconcileFailed", "Failed to reconcile RayService %v", err)
			return err
		}

		// mutate current.Spec to match desired svc.Spec
		current.Spec = rs.Spec
		// now try update
		controllerutil.SetOwnerReference(p, &current, b.Scheme)
		return b.Client.Update(ctx, &current)
	})
}

func (b *Builder) ReconcileRayAutoscalerRBAC(ctx context.Context, p *enterpriseApi.AIPlatform) error {
	logger := log.FromContext(ctx)
	saName := p.Spec.HeadGroupSpec.ServiceAccountName
	if saName == "" {
		logger.Info("No ServiceAccount specified for Ray head group, skipping RBAC reconciliation")
		return nil
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-autoscaler",
			Namespace: p.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"ray.io"},
				Resources: []string{"rayclusters", "rayservices", "rayjobs"},
				Verbs:     []string{"get", "list", "watch", "patch", "update", "delete"},
			},
		},
	}

	if err := b.Client.Create(ctx, role); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	controllerutil.SetOwnerReference(p, role, b.Scheme)

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-autoscaler-binding-" + p.Namespace + "-" + saName,
			Namespace: p.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: p.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "ray-autoscaler",
		},
	}

	if err := b.Client.Create(ctx, roleBinding); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	controllerutil.SetOwnerReference(p, roleBinding, b.Scheme)
	return nil
}

func (b *Builder) ReconcileRayServiceStatus(
	ctx context.Context,
	p *enterpriseApi.AIPlatform,
) error {
	// 1️⃣ fetch the up-to-date RayService
	rs := &rayv1.RayService{}
	key := types.NamespacedName{Namespace: p.Namespace, Name: p.Name}
	if err := b.Client.Get(ctx, key, rs); err != nil {
		return err
	}

	// 2️⃣ mirror its status into your CR
	p.Status.RayServiceStatus = rs.Status.ServiceStatus

	// Add Ray head service name to status
	p.Status.RayServiceName = fmt.Sprintf("%s-head-svc", p.Name)

	// 3️⃣ set a Condition based on whatever flag you like—e.g. the top-level Ready
	ready := metav1.ConditionFalse
	reason := "RayServiceStatus"
	msg := "ray service is not yet ready"
	if rs.Status.ServiceStatus == rayv1.Running {
		ready = metav1.ConditionTrue
		reason = "RayServiceReady"
		msg = "ray service is running"
	}

	cond := metav1.Condition{
		Type:               "RayServiceReady",
		Status:             ready,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&p.Status.Conditions, cond)

	return nil
}

// Build constructs a RayService resource based on the AI CR.
func (b *Builder) Build() *rayv1.RayService {
	rs := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.ai.Name,
			Namespace:   b.ai.Namespace,
			Annotations: b.ai.Annotations,
			Labels:      b.ai.Labels,
		},
		Spec: rayv1.RayServiceSpec{
			RayClusterSpec: b.buildClusterConfig(),
		},
	}
	return rs
}

func (b *Builder) buildClusterConfig() rayv1.RayClusterSpec {
	annotations, labels := buildHeadAnnotationsAndLabels(b.ai)
	head := rayv1.HeadGroupSpec{
		RayStartParams: map[string]string{
			"dashboard-host": "0.0.0.0",
			"num-cpus":       "0",
		},
		HeadService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        b.ai.Name + "-head-svc",
				Namespace:   b.ai.Namespace,
				Annotations: annotations,
				Labels:      labels,
			},
		},
		Template: b.makeHeadTemplate(),
	}

	head.Template.ObjectMeta.Annotations = annotations
	head.Template.ObjectMeta.Labels = labels

	var workers []rayv1.WorkerGroupSpec
	for i, cfg := range b.ai.Spec.WorkerGroupSpec.GPUConfigs {
		annotations, labels := buildWorkerAnnotationsAndLabels(b.ai, cfg)
		wg := rayv1.WorkerGroupSpec{
			GroupName:   cfg.Tier,
			MinReplicas: &cfg.MinReplicas,
			MaxReplicas: &cfg.MaxReplicas,
			RayStartParams: map[string]string{
				"resources": fmt.Sprintf(`"{\"accelerator_type:%s\":1,\"gpu_count:%d\":%d}"`, b.ai.Spec.DefaultAcceleratorType, i, cfg.GPUsPerPod),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
					Labels:      labels,
				},
				Spec: b.makeWorkerTemplate(cfg).Spec,
			},
		}
		workers = append(workers, wg)
	}

	return rayv1.RayClusterSpec{
		RayVersion:              os.Getenv("RAY_VERSION"),
		EnableInTreeAutoscaling: boolPtr(true),
		HeadGroupSpec:           head,
		WorkerGroupSpecs:        workers,
	}
}

func (b *Builder) makeHeadTemplate() corev1.PodTemplateSpec {
	spec := corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:  "ray-head",
			Image: SetImageRegistry("RELATED_IMAGE_RAY_HEAD", b.ai.Spec.HeadGroupSpec.ImageRegistry),
			Args: []string{
				"ulimit -n 65536; echo head; $KUBERAY_GEN_RAY_START_CMD",
			},
			Command: []string{
				"/bin/bash",
				"-lc",
				"--",
			},
			Env: []corev1.EnvVar{
				{Name: "DEFAULT_ACCELERATOR_TYPE", Value: b.ai.Spec.DefaultAcceleratorType},
				{Name: "CLUSTER_NAME", Value: os.Getenv("CLUSTER_NAME")},
			},
			Lifecycle: &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/bin/sh",
							"-c",
							"ray stop",
						},
					},
				},
			},
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: 6379,
					Name:          "gcs-server",
					Protocol:      corev1.ProtocolTCP,
				},
				{
					ContainerPort: 8265,
					Name:          "dashboard",
					Protocol:      corev1.ProtocolTCP,
				},
				{
					ContainerPort: 10001,
					Name:          "client",
					Protocol:      corev1.ProtocolTCP,
				},
				{
					ContainerPort: 8000,
					Name:          "serve",
					Protocol:      corev1.ProtocolTCP,
				},
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1"),
					corev1.ResourceMemory:           resource.MustParse("2Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("5Gi"),
					"nvidia.com/gpu":                resource.MustParse("0"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("4"),
					corev1.ResourceMemory:           resource.MustParse("8Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("10Gi"),
					"nvidia.com/gpu":                resource.MustParse("0"),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/tmp/ray",
					Name:      "ray-logs",
				},
			},
		}},
	}

	spec.NodeSelector = b.ai.Spec.HeadGroupSpec.NodeSelector
	spec.Tolerations = b.ai.Spec.HeadGroupSpec.Tolerations
	spec.Affinity = b.ai.Spec.HeadGroupSpec.Affinity
	spec.ServiceAccountName = b.ai.Spec.HeadGroupSpec.ServiceAccountName
	// FIXME need to find better way to add sidecars
	sidecars := sidecars.New(b.Client, b.Scheme, b.Recorder, b.ai)
	sidecars.AddFluentBitSidecar(&spec)
	return corev1.PodTemplateSpec{Spec: spec}
}

func (b *Builder) makeWorkerTemplate(cfg enterpriseApi.GPUConfig) corev1.PodTemplateSpec {
	rayCommand := fmt.Sprintf(`echo %s worker;
        ulimit -n 65536;
    	export PATH="/home/ray/anaconda3/bin:$PATH";
        KUBERAY_GEN_RAY_START_CMD=$(echo $KUBERAY_GEN_RAY_START_CMD | sed -e 's/"{/{/g' -e 's/}"/}/g' -e 's/\\\"/"/g');
        $KUBERAY_GEN_RAY_START_CMD;`, cfg.Tier)
	spec := corev1.PodSpec{
		ServiceAccountName: b.ai.Spec.WorkerGroupSpec.ServiceAccountName,
		Containers: []corev1.Container{{
			Name:            "ray-worker",
			Image:           SetImageRegistry("RELATED_IMAGE_RAY_WORKER", b.ai.Spec.WorkerGroupSpec.ImageRegistry),
			ImagePullPolicy: corev1.PullAlways,
			Command: []string{
				"/bin/bash",
				"-lc",
				"--",
			},
			Args: []string{
				rayCommand,
			},
			Env: []corev1.EnvVar{
				{Name: "DEFAULT_ACCELERATOR_TYPE", Value: b.ai.Spec.DefaultAcceleratorType},
				{Name: "RAY_HEAD_SERVICE_HOST", Value: fmt.Sprintf("%s.%s.svc.%s", b.ai.Name+"-head-svc", b.ai.Namespace, os.Getenv("CLUSTER_DOMAIN"))},
				{Name: "SERVICE_NAME", Value: b.ai.Name},
				{Name: "SERVICE_INTERNAL_NAME", Value: b.ai.Name},
				{Name: "USE_SYSTEM_PERMISSIONS", Value: "true"},
				{Name: "GPG_PUBLICKEY_PATH", Value: "kv-splunk/al-platform.ray-worker-sa/gpgkey"}, // FIXME
				{Name: "GPU_TYPE", Value: "L40S"},                                                 // FIXME
				{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"},
			},
			Lifecycle: &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/bin/sh",
							"-c",
							"ray stop",
						},
					},
				},
			},
			Resources: cfg.Resources,
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/tmp/ray",
					Name:      "ray-logs",
				},
			},
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: 8080,
					Name:          "metrics",
					Protocol:      corev1.ProtocolTCP,
				},
			},
		}},
	}

	// apply scheduling
	spec.NodeSelector = b.ai.Spec.WorkerGroupSpec.NodeSelector
	spec.Tolerations = b.ai.Spec.WorkerGroupSpec.Tolerations
	spec.Affinity = b.ai.Spec.WorkerGroupSpec.Affinity

	found := false
	for _, vol := range spec.Volumes {
		if vol.Name == "ray-logs" {
			found = true
			break
		}
	}

	if !found {
		spec.Volumes = append(spec.Volumes, corev1.Volume{
			Name: "ray-logs",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
	// FIXME need to find better way to add sidecars
	sidecars := sidecars.New(b.Client, b.Scheme, b.Recorder, b.ai)
	sidecars.AddFluentBitSidecar(&spec)

	return corev1.PodTemplateSpec{Spec: spec}
}

func SetImageRegistry(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func buildWorkerAnnotationsAndLabels(aiPlatform *enterpriseApi.AIPlatform, cfg enterpriseApi.GPUConfig) (map[string]string, map[string]string) {
	annotations := make(map[string]string)
	labels := make(map[string]string)

	// Example: propagate tier and GPU type as labels/annotations
	annotations["gpu-tier"] = cfg.Tier
	labels["gpu-tier"] = cfg.Tier
	if aiPlatform.Annotations != nil {
		for k, v := range aiPlatform.Annotations {
			if strings.Contains(k, "last-applied-configuration") {
				continue
			}
			annotations[k] = v
		}
	}
	if aiPlatform.Labels != nil {
		for k, v := range aiPlatform.Labels {
			if strings.Contains(k, "last-applied-configuration") {
				continue
			}
			labels[k] = v
		}
	}
	annotations["prometheus.io/path"] = "/metrics"
	annotations["prometheus.io/port"] = "8080"
	annotations["prometheus.io/scheme"] = "http"
	annotations["ray.io/overwrite-container-cmd"] = "true"
	if aiPlatform.Spec.Sidecars.Otel {
		annotations["sidecar.opentelemetry.io/inject"] = fmt.Sprintf("%s-otel-coll", aiPlatform.Name)
		annotations["sidecar.opentelemetry.io/auto-instrument"] = "true"
	}

	// Add any additional logic as needed

	return annotations, labels
}

func buildHeadAnnotationsAndLabels(aiPlatform *enterpriseApi.AIPlatform) (map[string]string, map[string]string) {
	annotations := make(map[string]string)
	labels := make(map[string]string)

	// Example: propagate tier and GPU type as labels/annotations
	if aiPlatform.Annotations != nil {
		for k, v := range aiPlatform.Annotations {
			if strings.Contains(k, "last-applied-configuration") {
				continue
			}
			annotations[k] = v
		}
	}
	if aiPlatform.Labels != nil {
		for k, v := range aiPlatform.Labels {
			if strings.Contains(k, "last-applied-configuration") {
				continue
			}
			labels[k] = v
		}
	}
	annotations["prometheus.io/path"] = "/metrics"
	annotations["prometheus.io/port"] = "8080"
	annotations["prometheus.io/scheme"] = "http"
	annotations["ray.io/overwrite-container-cmd"] = "true"

	if aiPlatform.Spec.Sidecars.Otel {
		annotations["sidecar.opentelemetry.io/inject"] = fmt.Sprintf("%s-otel-coll", aiPlatform.Name)
		annotations["sidecar.opentelemetry.io/auto-instrument"] = "true"
	}

	return annotations, labels
}

// boolPtr returns a pointer to the given boolean value.
func boolPtr(b bool) *bool {
	return &b
}
