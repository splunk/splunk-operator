// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Package main demonstrates basic Platform SDK usage for a Splunk Standalone controller.
//
// This example shows:
// - Setting up the SDK runtime
// - Using ReconcileContext in reconcile loop
// - Certificate and secret resolution
// - Building Kubernetes resources with fluent API
// - Event recording and structured logging
package main

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	sdk "github.com/splunk/splunk-operator/pkg/platform-sdk"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
)

// StandaloneReconciler reconciles a Splunk Standalone instance.
type StandaloneReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	sdkRuntime api.Runtime
}

// Standalone is a simplified CRD for this example.
type Standalone struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StandaloneSpec `json:"spec,omitempty"`
}

type StandaloneSpec struct {
	Replicas int32  `json:"replicas"`
	Image    string `json:"image"`
}

func (s *Standalone) DeepCopyObject() runtime.Object {
	return &Standalone{
		TypeMeta:   s.TypeMeta,
		ObjectMeta: *s.ObjectMeta.DeepCopy(),
		Spec:       s.Spec,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *StandaloneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logger := log.FromContext(context.Background())

	// Create event recorder
	recorder := mgr.GetEventRecorderFor("splunk-operator")

	// Create SDK runtime
	sdkRuntime, err := sdk.NewRuntime(
		mgr.GetClient(),
		sdk.WithClusterScoped(),
		sdk.WithLogger(logger),
		sdk.WithEventRecorder(recorder),
	)
	if err != nil {
		return fmt.Errorf("failed to create SDK runtime: %w", err)
	}

	// Start the SDK (initializes services, starts watchers)
	if err := sdkRuntime.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start SDK runtime: %w", err)
	}

	r.sdkRuntime = sdkRuntime

	logger.Info("Platform SDK initialized successfully")

	return ctrl.NewControllerManagedBy(mgr).
		For(&Standalone{}).
		Complete(r)
}

// Reconcile implements the reconciliation logic.
func (r *StandaloneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create SDK ReconcileContext for this reconciliation
	rctx := r.sdkRuntime.NewReconcileContext(ctx, req.Namespace, req.Name)

	rctx.Logger().Info("Starting reconciliation")

	// Fetch the Standalone CR
	standalone := &Standalone{}
	if err := r.Get(ctx, req.NamespacedName, standalone); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 1: Resolve TLS certificate
	rctx.Logger().V(1).Info("Resolving TLS certificate")

	duration := 90 * 24 * time.Hour // 90 days
	cert, err := rctx.ResolveCertificate(certificate.Binding{
		Name: fmt.Sprintf("%s-tls", standalone.Name),
		DNSNames: []string{
			fmt.Sprintf("%s.%s.svc", standalone.Name, standalone.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", standalone.Name, standalone.Namespace),
		},
		Duration: &duration,
	})
	if err != nil {
		rctx.Logger().Error(err, "Failed to resolve certificate")
		return ctrl.Result{}, err
	}

	if !cert.Ready {
		rctx.Logger().Info("Certificate not ready, requeueing",
			"provider", cert.Provider,
			"secretName", cert.SecretName)

		// Record event about certificate provisioning
		rctx.EventRecorder().Event(standalone, corev1.EventTypeNormal,
			"CertificateProvisioning",
			fmt.Sprintf("Waiting for certificate %s from %s", cert.SecretName, cert.Provider))

		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	rctx.Logger().Info("Certificate ready",
		"provider", cert.Provider,
		"secretName", cert.SecretName)

	// Record event about certificate being ready
	rctx.EventRecorder().Event(standalone, corev1.EventTypeNormal,
		api.EventReasonCertificateReady,
		fmt.Sprintf("Certificate %s is ready", cert.SecretName))

	// Step 2: Resolve Splunk credentials secret
	rctx.Logger().V(1).Info("Resolving Splunk credentials")

	secretRef, err := rctx.ResolveSecret(secret.Binding{
		Name: fmt.Sprintf("%s-credentials", standalone.Name),
		Type: secret.SecretTypeSplunk,
		Keys: []string{"password", "hec_token"},
	})
	if err != nil {
		rctx.Logger().Error(err, "Failed to resolve secret")
		return ctrl.Result{}, err
	}

	if !secretRef.Ready {
		rctx.Logger().Info("Secret not ready", "secretName", secretRef.SecretName)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if secretRef.Version != nil {
		rctx.Logger().V(1).Info("Using versioned secret",
			"version", *secretRef.Version,
			"secretName", secretRef.SecretName)
	}

	// Step 3: Build ConfigMap with Splunk configuration
	rctx.Logger().V(1).Info("Building ConfigMap")

	configMap, err := rctx.BuildConfigMap().
		WithName(fmt.Sprintf("%s-config", standalone.Name)).
		WithData(map[string]string{
			"default.yml": `
splunk:
  hec:
    enable: true
    port: 8088
  s2s:
    enable: true
    port: 9997
`,
		}).
		Build()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply ConfigMap
	if err := r.Patch(ctx, configMap, client.Apply,
		client.ForceOwnership,
		client.FieldOwner("splunk-operator")); err != nil {
		return ctrl.Result{}, err
	}

	// Step 4: Build StatefulSet
	rctx.Logger().V(1).Info("Building StatefulSet")

	sts, err := rctx.BuildStatefulSet().
		WithName(standalone.Name).
		WithReplicas(standalone.Spec.Replicas).
		WithImage(standalone.Spec.Image).
		WithPorts([]corev1.ContainerPort{
			{Name: "web", ContainerPort: 8000, Protocol: corev1.ProtocolTCP},
			{Name: "mgmt", ContainerPort: 8089, Protocol: corev1.ProtocolTCP},
			{Name: "hec", ContainerPort: 8088, Protocol: corev1.ProtocolTCP},
			{Name: "s2s", ContainerPort: 9997, Protocol: corev1.ProtocolTCP},
		}).
		WithCertificate(cert). // Auto-mounts certificate at /etc/certs/{secretName}
		WithSecret(secretRef). // Auto-creates volume for secret
		WithConfigMap(configMap.Name).
		WithEnv(corev1.EnvVar{
			Name:  "SPLUNK_START_ARGS",
			Value: "--accept-license",
		}).
		WithEnv(corev1.EnvVar{
			Name: "SPLUNK_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretRef.SecretName,
					},
					Key: "password",
				},
			},
		}).
		WithResources(corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		}).
		WithObservability(). // Adds Prometheus scraping annotations
		Build()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply StatefulSet
	if err := r.Patch(ctx, sts, client.Apply,
		client.ForceOwnership,
		client.FieldOwner("splunk-operator")); err != nil {
		return ctrl.Result{}, err
	}

	rctx.Logger().Info("StatefulSet applied", "name", sts.Name)

	// Step 5: Build Service
	rctx.Logger().V(1).Info("Building Service")

	service, err := rctx.BuildService().
		WithName(standalone.Name).
		WithType(corev1.ServiceTypeClusterIP).
		WithPorts([]corev1.ServicePort{
			{
				Name:       "web",
				Port:       8000,
				TargetPort: intstr.FromInt(8000),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "mgmt",
				Port:       8089,
				TargetPort: intstr.FromInt(8089),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "hec",
				Port:       8088,
				TargetPort: intstr.FromInt(8088),
				Protocol:   corev1.ProtocolTCP,
			},
		}).
		WithDiscoveryLabels(). // Enables service discovery
		Build()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply Service
	if err := r.Patch(ctx, service, client.Apply,
		client.ForceOwnership,
		client.FieldOwner("splunk-operator")); err != nil {
		return ctrl.Result{}, err
	}

	rctx.Logger().Info("Service applied", "name", service.Name)

	// Record successful reconciliation event
	rctx.EventRecorder().Event(standalone, corev1.EventTypeNormal,
		"ReconciliationComplete",
		"Splunk Standalone has been successfully reconciled")

	rctx.Logger().Info("Reconciliation complete")

	return ctrl.Result{}, nil
}

func main() {
	// This is just an example showing SDK usage patterns.
	// In a real controller, you would call SetupWithManager from main.go
	fmt.Println("This is an example showing Platform SDK usage")
	fmt.Println("See the code for complete patterns and best practices")
}
