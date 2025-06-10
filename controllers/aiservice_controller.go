/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
... (license text omitted for brevity) ...
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	//"github.com/splunk/splunk-operator/controllers/ai_platform/sidecars"
)

var (
	reconcileHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "splunk_ai_assistant_reconcile_duration_seconds",
			Help: "Duration of reconcile stages",
		},
		[]string{"stage"},
	)
)

func init() {
	metrics.Registry.MustRegister(reconcileHistogram)
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=aiservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=aiservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=aiservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opentelemetry.io,resources=opentelemetrycollectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// AIServiceReconciler reconciles a AIService object
type AIServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile runs reconciliation stages for the CR.
func (r *AIServiceReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	ai := &enterpriseApi.AIService{}
	if err := r.Get(ctx, req.NamespacedName, ai); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var conditions []metav1.Condition
	defer func() {
		ai.Status.Conditions = conditions
		ai.Status.ObservedGeneration = ai.Generation
		_ = r.Status().Update(ctx, ai)
	}()

	stages := []struct {
		name string
		fn   func(context.Context, *enterpriseApi.AIService) error
	}{
		{"Validate", r.validateAIService},
		{"ServiceAccount", r.reconcileServiceAccount},
		{"FluentBitConfig", r.reconcileFluentBitConfig},
		{"Certificate", r.reconcileCertificate},
		{"PostInstallHook", r.reconcilePostInstallHook},
		{"SAIADeployment", r.reconcileSAIADeployment},
		{"SAIAService", r.reconcileSAIAService},
		{"ServiceMonitor", r.reconcileServiceMonitor},
	}

	for _, stage := range stages {
		start := time.Now()
		err := stage.fn(ctx, ai)
		reconcileHistogram.WithLabelValues(stage.name).Observe(
			time.Since(start).Seconds(),
		)

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
			//r.Recorder.Event(ai, corev1.EventTypeWarning, stage.name+"Failed", err.Error())
		} else {
			//		r.Recorder.Event(ai, corev1.EventTypeNormal, stage.name+"Succeeded", "stage succeeded")
		}
		conditions = append(conditions, cond)
		if err != nil {
			log.Error(err, "stage failed", "stage", stage.name)
			return ctrl.Result{}, err
		}
	}

	conditions = append(conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "AllReconciled",
		Message:            "all stages completed successfully",
		LastTransitionTime: metav1.Now(),
	})

	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller and owned resources.
func (r *AIServiceReconciler) SetupWithManager(
	mgr ctrl.Manager,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.AIService{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&certmanagerv1.Certificate{}).
		Owns(&batchv1.Job{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&monitoringv1.ServiceMonitor{}).
		Complete(r)
}

// validateAIService ensures required fields are set and defaults.
func (r *AIServiceReconciler) validateAIService(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	if os.Getenv("RELATED_IMAGE_POST_INSTALL_HOOK") == "" {
		return fmt.Errorf("RELATED_IMAGE_POST_INSTALL_HOOK must be set")
	}
	// Populate URLs from AIPlatformRef if provided
	if ai.Spec.AIPlatformRef.Name != "" {
		plat := &enterpriseApi.AIPlatform{}
		if err := r.Get(
			ctx,
			client.ObjectKey{Namespace: ai.Namespace, Name: ai.Spec.AIPlatformRef.Name},
			plat,
		); err != nil {
			return fmt.Errorf("fetching AIPlatform: %w", err)
		}
		ai.Spec.AIPlatformUrl = fmt.Sprintf("%s.%s.svc.%s:8000", plat.Status.RayServiceName, ai.Spec.AIPlatformRef.Namespace, "cluster.local")
		ai.Spec.VectorDbUrl = fmt.Sprintf("%s.%s.svc.%s", plat.Status.VectorDbServiceName, ai.Spec.AIPlatformRef.Namespace, "cluster.local")
	}
	if ai.Spec.AIPlatformRef.Name == "" && ai.Spec.AIPlatformUrl == "" {
		return fmt.Errorf(
			"either AIPlatformRef.Name or AIPlatformUrl must be set",
		)
	}
	if ai.Spec.AIPlatformUrl == "" && ai.Spec.VectorDbUrl == "" {
		return fmt.Errorf(
			"either AIPlatformUrl or VectorDbUrl must be set",
		)
	}
	// Default resources
	if ai.Spec.Resources.Requests == nil {
		ai.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}
	}
	if ai.Spec.Resources.Limits == nil {
		ai.Spec.Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		}
	}
	if ai.Spec.TaskVolume.Path == "" {
		return fmt.Errorf("task volume path must be set")
	}
	if ai.Spec.Replicas == 0 {
		ai.Spec.Replicas = 1
	}
	return nil
}

// reconcileServiceAccount creates or reuses a ServiceAccount.
func (r *AIServiceReconciler) reconcileServiceAccount(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	if ai.Spec.ServiceAccountName == "" {

		ai.Spec.ServiceAccountName = ai.Name + "-sa"
		if err := r.Update(ctx, ai); err != nil {
			return fmt.Errorf("updating SA name in spec: %w", err)
		}
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ai.Spec.ServiceAccountName,
				Namespace: ai.Namespace,
			},
		}
		if err := controllerutil.SetControllerReference(ai, sa, r.Scheme); err != nil {
			return fmt.Errorf("ownerref on SA: %w", err)
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
			return nil
		}); err != nil {
			return fmt.Errorf("create/update SA: %w", err)
		}
	}
	return nil
}

// reconcileCertificate manages cert-manager Certificate for mTLS.
func (r *AIServiceReconciler) reconcileCertificate(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	if !ai.Spec.MTLS.Enabled || ai.Spec.MTLS.Termination != "operator" {
		return nil
	}
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ai.Name + "-tls",
			Namespace: ai.Namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: ai.Spec.MTLS.SecretName,
			IssuerRef:  ai.Spec.MTLS.IssuerRef,
			DNSNames:   ai.Spec.MTLS.DNSNames,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
			},
		},
	}
	if err := controllerutil.SetControllerReference(ai, cert, r.Scheme); err != nil {
		return fmt.Errorf("ownerref on Certificate: %w", err)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		return nil
	}); err != nil {
		return fmt.Errorf("create/update Certificate: %w", err)
	}
	// Wait until Certificate is Ready
	for _, cond := range cert.Status.Conditions {
		if cond.Type == certmanagerv1.CertificateConditionReady && cond.Status == cmmeta.ConditionTrue {
			return nil
		}
	}
	return fmt.Errorf("waiting for Certificate %q to become Ready", cert.Name)
}

// reconcilePostInstallHook creates and watches the schema setup Job.
func (r *AIServiceReconciler) reconcilePostInstallHook(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	hookImage := os.Getenv("RELATED_IMAGE_POST_INSTALL_HOOK")
	if ai.Spec.VectorDbUrl == "" {
		return nil
	}
	if ai.Status.SchemaJobId != "" {
		job := &batchv1.Job{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: ai.Namespace, Name: ai.Status.SchemaJobId},
			job,
		)
		if apierrors.IsNotFound(err) {
			ai.Status.SchemaJobId = ""
		} else if err != nil {
			return fmt.Errorf("fetching Job: %w", err)
		} else {
			for _, c := range job.Status.Conditions {
				if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
					return nil
				}
				if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
					return fmt.Errorf("Job %q failed", job.Name)
				}
			}
			return fmt.Errorf("job %q is still running", job.Name)
		}
	}
	uri := fmt.Sprintf("http://%s:80", ai.Spec.VectorDbUrl)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ai.Name + "-vector-db-setup-posthook",
			Namespace: ai.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "vector-db-setup-container",
							Image:           hookImage,
							ImagePullPolicy: corev1.PullAlways,
							Env: []corev1.EnvVar{
								{Name: "VECTOR_DB_URL", Value: uri},
							},
						},
					},
					Tolerations: ai.Spec.Tolerations,
					Affinity:    &ai.Spec.Affinity,
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(ai, job, r.Scheme); err != nil {
		return fmt.Errorf("ownerref on Job: %w", err)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, job, func() error { return nil }); err != nil {
		return fmt.Errorf("create/update Job: %w", err)
	}
	ai.Status.SchemaJobId = job.Name
	return fmt.Errorf("created Job %q, waiting for completion", job.Name)
}

// reconcileSAIADeployment ensures the main Deployment exists and is configured.
func (r *AIServiceReconciler) reconcileSAIADeployment(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	// Build volumes
	volumes := []corev1.Volume{
		{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "features-config"},
					Optional:             pointer.BoolPtr(true),
				},
			},
		},
	}
	// Build ports and env
	ports := []corev1.ContainerPort{
		{Name: "http", ContainerPort: 8080},
		{Name: "metrics", ContainerPort: 8088},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config-volume", MountPath: "/etc/config"},
	}
	env := []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "info"}}
	if ai.Spec.MTLS.Enabled && ai.Spec.MTLS.Termination == "operator" {
		volumes = append(volumes,
			corev1.Volume{
				Name: "tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: ai.Spec.MTLS.SecretName},
				},
			},
		)
		mounts = append(mounts, corev1.VolumeMount{Name: "tls", MountPath: "/etc/tls", ReadOnly: true})
		env = append(env,
			corev1.EnvVar{Name: "TLS_CERT_FILE", Value: "/etc/tls/tls.crt"},
			corev1.EnvVar{Name: "TLS_KEY_FILE", Value: "/etc/tls/tls.key"},
		)
		ports = append(ports, corev1.ContainerPort{Name: "https", ContainerPort: 8443})
	} else {
		env = append(env, corev1.EnvVar{Name: "TLS_DISABLED", Value: "true"})
	}

	// Add required env variables
	extraEnv := []corev1.EnvVar{
		{Name: "IAC_URL", Value: "test.iac.url"},                   //FIXME remove this
		{Name: "API_GATEWAY_HOST", Value: "test.api.gateway.host"}, //FIXME remove this
		{Name: "SCPAUTH_SECRET_PATH", Value: "stest-secret-path"},  //FIXME remove this
		{Name: "AUTH_PROVIDER", Value: "scp"},                      // FIXME remove this
		{Name: "ENABLE_AUTHZ", Value: "false"},                     //FIXME remove this
		{Name: "FEATURE_CONFIG_FILE_LOCATION", Value: "/etc/config/features_config.yaml"},
		{Name: "PLATFORM_URL", Value: ai.Spec.AIPlatformUrl},
		{Name: "PLATFORM_VERSION", Value: "0.3.0"},                                                   // TODO : make this configurable
		{Name: "SAIA_API_VERSION", Value: "0.3.1"},                                                   // TODO : make this configurable
		{Name: "TASK_RUNNER_BACKUP_ENABLED", Value: "false"},                                         // TODO : make this configurable
		{Name: "TELEMETRY_ENV", Value: "prod"},                                                       // TODO: make this configurable
		{Name: "TELEMETRY_REGION", Value: "region-us-west-2"},                                        // TODO: make this configurable
		{Name: "TELEMETRY_TENANT", Value: "test"},                                                    // TODO: make this configurable
		{Name: "TELEMETRY_URL", Value: "https://telemetry-splkmobile.dataeng.splunk.com/2.0/events"}, // TODO: make this configurable
		{Name: "WEAVIATE_API_URL", Value: ai.Spec.VectorDbUrl},
		{Name: "STORAGE_URL", Value: ai.Spec.TaskVolume.Path}, // TODO : make this configurable , not sure why we need this
		{Name: "S3_BUCKET", Value: ai.Spec.TaskVolume.Path},   // TODO : make this configurable , not sure why we need this
	}
	env = append(env, extraEnv...)
	// Sort env variables by Name for deterministic ordering
	sort.Slice(env, func(i, j int) bool {
		return env[i].Name < env[j].Name
	})

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ai.Name + "-saia-deployment",
			Namespace: ai.Namespace,
			Labels: map[string]string{
				"app":       ai.Name,
				"component": ai.Name,
				"area":      "ml",
				"team":      "ml",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &ai.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": ai.Name, "component": ai.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": ai.Name, "component": ai.Name},
					Annotations: map[string]string{
						"prometheus.io/port":   "8088", //FIXME only enable when metrics are enabled
						"prometheus.io/path":   "/metrics",
						"prometheus.io/scheme": "http",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ai.Spec.ServiceAccountName,
					Containers: []corev1.Container{{
						Name:            ai.Name,
						Image:           os.Getenv("RELATED_IMAGE_SAIA_API"),
						ImagePullPolicy: corev1.PullAlways,
						Ports:           ports,
						VolumeMounts:    mounts,
						Resources:       ai.Spec.Resources,
						Env:             env,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(8080)},
							},
							PeriodSeconds:    30,
							FailureThreshold: 5,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(8080)},
							},
							PeriodSeconds:    30,
							FailureThreshold: 5,
						},
						StartupProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(8080)},
							},
							InitialDelaySeconds: 10,
							PeriodSeconds:       30,
							FailureThreshold:    5,
						},
					}},
					Volumes:     volumes,
					Affinity:    &ai.Spec.Affinity,
					Tolerations: ai.Spec.Tolerations,
				},
			},
		},
	}
	// Merge ai.Labels into deployment.ObjectMeta.Labels
	for k, v := range ai.Labels {
		deployment.ObjectMeta.Labels[k] = v
	}
	for k, v := range ai.Annotations {
		if k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		} // Ignore last-applied-configuration annotation
		if k == "kubectl.kubernetes.io/restartedAt" {
			continue
		} // Ignore restartedAt annotation
		deployment.ObjectMeta.Annotations[k] = v
	}
	// FIXME need to find better way to add sidecars
	// sidecars arguments need to be changed to take abstract class
	//sidecars := sidecars.New(r.Client, r.Scheme, r.Recorder, ai)
	r.AddFluentBitSidecar(&deployment.Spec.Template.Spec, ai)

	if err := controllerutil.SetControllerReference(ai, deployment, r.Scheme); err != nil {
		return fmt.Errorf("ownerref on Deployment: %w", err)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error { return nil }); err != nil {
		return fmt.Errorf("create/update Deployment: %w", err)
	}
	return nil
}

// reconcileSAIAService ensures the Service for SAIA is created/updated. // remove me
func (r *AIServiceReconciler) reconcileSAIAService(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	ports := []corev1.ServicePort{
		{Name: "http", Port: 8080, TargetPort: intstr.FromInt(8080)},
		{Name: "metrics", Port: 8088, TargetPort: intstr.FromInt(8088)},
	}
	if ai.Spec.MTLS.Enabled && ai.Spec.MTLS.Termination == "operator" {
		ports = append(ports, corev1.ServicePort{
			Name: "https", Port: 8443, TargetPort: intstr.FromInt(8443),
		})
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ai.Name + "-saia-service",
			Namespace: ai.Namespace,
			Labels:    map[string]string{"app": ai.Name},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": ai.Name, "component": ai.Name},
			Ports:    ports,
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
	for k, v := range ai.Labels {
		svc.ObjectMeta.Labels[k] = v
	}
	for k, v := range ai.Annotations {
		if k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		} // Ignore last-applied-configuration annotation
		if k == "kubectl.kubernetes.io/restartedAt" {
			continue
		} // Ignore restartedAt annotation
		svc.ObjectMeta.Annotations[k] = v
	}

	if ai.Spec.ServiceTemplate.Spec.Type == corev1.ServiceTypeLoadBalancer {
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
	} else if ai.Spec.ServiceTemplate.Spec.Type == corev1.ServiceTypeNodePort {
		svc.Spec.Type = corev1.ServiceTypeNodePort
		// If NodePort values are specified, set them
		for i, port := range svc.Spec.Ports {
			for _, tplPort := range ai.Spec.ServiceTemplate.Spec.Ports {
				if port.Name == tplPort.Name && tplPort.NodePort != 0 {
					svc.Spec.Ports[i].NodePort = tplPort.NodePort
				}
			}
		}
	} else {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
	}

	if err := controllerutil.SetControllerReference(ai, svc, r.Scheme); err != nil {
		return fmt.Errorf("ownerref on Service: %w", err)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error { return nil }); err != nil {
		return fmt.Errorf("create/update Service: %w", err)
	}
	return nil
}

// reconcileServiceMonitor creates a Prometheus ServiceMonitor if metrics are enabled.
func (r *AIServiceReconciler) reconcileServiceMonitor(
	ctx context.Context,
	ai *enterpriseApi.AIService,
) error {
	if !ai.Spec.Metrics.Enabled {
		return nil
	}
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: ai.Name + "-metrics", Namespace: ai.Namespace},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": ai.Name, "component": ai.Name},
			},
			Endpoints: []monitoringv1.Endpoint{
				{Port: "metrics", Path: ai.Spec.Metrics.Path, Scheme: "http"},
			},
		},
	}
	if err := controllerutil.SetControllerReference(ai, sm, r.Scheme); err != nil {
		return err
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sm, func() error { return nil })
	return err
}

// reconcileFluentBitConfig ensures the FluentBit sidecar ConfigMap exists and is up-to-date // remove me
func (r *AIServiceReconciler) reconcileFluentBitConfig(ctx context.Context, p *enterpriseApi.AIService) error {
	// Retrieve the secret reference from SplunkConfiguration
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      p.Spec.SplunkConfiguration.SecretRef.Name,
		Namespace: p.Namespace,
	}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to retrieve secret %q: %w", secretKey.Name, err)
	}

	// Extract the HEC token from the secret
	hecToken, exists := secret.Data["hec_token"]
	if !exists {
		return fmt.Errorf("hec_token not found in secret %q", secretKey.Name)
	}

	// Retrieve the endpoint from SplunkConfiguration
	endpoint := p.Spec.SplunkConfiguration.Endpoint
	if endpoint == "" {
		return fmt.Errorf("endpoint is not specified in SplunkConfiguration")
	}

	fluentbitConfig := fmt.Sprintf(renderFluentBitConf(), endpoint, string(hecToken))
	// Update FluentBit configuration with the retrieved values
	data := map[string]string{
		"fluent-bit.conf": fluentbitConfig,
		"parser.conf":     renderParserConf(),
	}

	cmName := fmt.Sprintf("%s-fluentbit-config", p.Name)
	err := r.createOrUpdateConfigMap(ctx, cmName, data, p)
	if err != nil {
		return err
	}

	// Validate the ConfigMap before returning
	found := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: p.Namespace}, found)
	if err != nil {
		return fmt.Errorf("failed to validate ConfigMap %q: %w", cmName, err)
	}
	return nil
}

func (r *AIServiceReconciler) AddFluentBitSidecar(podSpec *corev1.PodSpec, ai *enterpriseApi.AIService) {
	// Add FluentBit sidecar if enabled and not already present

	found := false
	for _, container := range podSpec.Containers {
		if container.Name == "fluentbit" {
			found = true
			break
		}
	}
	if !found {
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:  "fluentbit",
			Image: "fluent/fluent-bit:1.9.6",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/fluent-bit/etc/parser.conf",
					SubPath:   "parser.conf",
					Name:      "fluentbit-config",
				},
				{
					MountPath: "/fluent-bit/etc/fluent-bit.conf",
					SubPath:   "fluent-bit.conf",
					Name:      "fluentbit-config",
				},
			},
		})

	}
	found = false
	for _, volume := range podSpec.Volumes {
		if volume.Name == "fluentbit-config" {
			found = true
			break
		}
	}
	if !found {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "fluentbit-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-fluentbit-config", ai.Name),
					},
				},
			},
		})
	}
}

// createOrUpdateConfigMap is a helper to create or patch a ConfigMap // remove me
func (r *AIServiceReconciler) createOrUpdateConfigMap(
	ctx context.Context,
	name string,
	data map[string]string,
	ai *enterpriseApi.AIService,
) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ai.Namespace,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(ai, cm, r.Scheme); err != nil {
		return err
	}

	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ai.Namespace}, found)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(found.Data, data) {
		found.Data = data
		return r.Update(ctx, found)
	}
	return nil
}

// renderFluentBitConf generates the FluentBit configuration for the given RayService.
func renderFluentBitConf() string {
	return `
	[SERVICE]
        Parsers_File /fluent-bit/etc/parser.conf
    [INPUT]
        Name tail
        Path /tmp/ray/session_latest/logs/*, /tmp/ray/session_latest/logs/*/*
        Tag ray
        Path_Key source_log_file_path
        Refresh_Interval 5
        Parser colon_prefix_parser
    [FILTER]
        Name                modify
        Match               ray
        Add                 application_name NONE
        Add                 deployment_name NONE
    [OUTPUT]
        Name stdout
        Format json_lines
        Match *
    [OUTPUT]
        Name   splunk
        Match  *
        Host   "%s"
        Splunk_Token  %s
        TLS    On
        TLS.verify  Off
`
}

// renderParserConf generates the parser configuration for FluentBit.
func renderParserConf() string {
	return `
	[PARSER]
        Name                colon_prefix_parser
        Format              regex
        Regex               :actor_name:ServeReplica:(?<application_name>[a-zA-Z0-9_-]+):(?<deployment_name>[a-zA-Z0-9_-]+)
        Time_Key            time
        Time_Format         %Y-%m-%dT%H:%M:%S
`
}
