package genai

import (
	"context"
	"fmt"
	"reflect"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	rayv1 "github.com/splunk/splunk-operator/controllers/ray/v1"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RayServiceReconciler is an interface for reconciling the RayService and its associated resources.
type RayServiceReconciler interface {
	Reconcile(ctx context.Context) (enterpriseApi.RayClusterStatus, error)
	ReconcileRayCluster(ctx context.Context) error
	ReconcileConfigMap(ctx context.Context) error
	ReconcileSecret(ctx context.Context) error
	ReconcileService(ctx context.Context) error
}

// rayServiceReconcilerImpl is the concrete implementation of RayServiceReconciler.
type rayServiceReconcilerImpl struct {
	client.Client
	genAIDeployment *enterpriseApi.GenAIDeployment
	eventRecorder   *splutil.K8EventPublisher
}

// NewRayServiceReconciler creates a new instance of RayServiceReconciler.
func NewRayServiceReconciler(c client.Client, genAIDeployment *enterpriseApi.GenAIDeployment, eventRecorder *splutil.K8EventPublisher) RayServiceReconciler {
	return &rayServiceReconcilerImpl{
		Client:          c,
		genAIDeployment: genAIDeployment,
		eventRecorder:   eventRecorder,
	}
}

func (r *rayServiceReconcilerImpl) ReconcileRayCluster(ctx context.Context) error {
	labels := map[string]string{
		"app":        "ray-service",
		"deployment": r.genAIDeployment.Name,
	}

	rayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ray-cluster", r.genAIDeployment.Name),
			Namespace: r.genAIDeployment.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: r.genAIDeployment.Spec.RayService.Image,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse(r.genAIDeployment.Spec.RayService.HeadGroup.NumCpus),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Replicas: &r.genAIDeployment.Spec.RayService.WorkerGroup.Replicas,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "ray-worker",
									Image:     r.genAIDeployment.Spec.RayService.Image,
									Resources: r.genAIDeployment.Spec.RayService.WorkerGroup.Resources,
								},
							},
						},
					},
				},
			},
		},
	}

	existingRayCluster := &rayv1.RayCluster{}
	err := r.Get(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, existingRayCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		if err := r.Create(ctx, rayCluster); err != nil {
			return fmt.Errorf("failed to create RayCluster: %w", err)
		}
	} else if !reflect.DeepEqual(rayCluster.Spec, existingRayCluster.Spec) {
		existingRayCluster.Spec = rayCluster.Spec
		if err := r.Update(ctx, existingRayCluster); err != nil {
			return fmt.Errorf("failed to update RayCluster: %w", err)
		}
	}
	return nil
}

func (r *rayServiceReconcilerImpl) ReconcileConfigMap(ctx context.Context) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-service-config",
			Namespace: r.genAIDeployment.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Data: map[string]string{
			"rayConfigKey": "rayConfigValue",
		},
	}

	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: configMap.Name, Namespace: configMap.Namespace}, existingConfigMap)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		if err := r.Create(ctx, configMap); err != nil {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
	}
	return nil
}

func (r *rayServiceReconcilerImpl) ReconcileSecret(ctx context.Context) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-service-secret",
			Namespace: r.genAIDeployment.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Data: map[string][]byte{
			"authKey": []byte("your-secret-auth-key"),
		},
	}

	existingSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: secret.Name, Namespace: secret.Namespace}, existingSecret)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		if err := r.Create(ctx, secret); err != nil {
			return fmt.Errorf("failed to create Secret: %w", err)
		}
	}
	return nil
}

func (r *rayServiceReconcilerImpl) ReconcileService(ctx context.Context) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ray-service", r.genAIDeployment.Name),
			Namespace: r.genAIDeployment.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":        "ray-service",
				"deployment": r.genAIDeployment.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:     6379,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, existingService)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		if err := r.Create(ctx, service); err != nil {
			return fmt.Errorf("failed to create Service: %w", err)
		}
	} else if !reflect.DeepEqual(service.Spec, existingService.Spec) {
		existingService.Spec = service.Spec
		if err := r.Update(ctx, existingService); err != nil {
			return fmt.Errorf("failed to update Service: %w", err)
		}
	}
	return nil
}

// Reconcile manages the complete reconciliation logic for the RayService and returns its status.
func (r *rayServiceReconcilerImpl) Reconcile(ctx context.Context) (enterpriseApi.RayClusterStatus, error) {
	status := enterpriseApi.RayClusterStatus{}

	// Reconcile the RayCluster resource for RayService
	if err := r.ReconcileRayCluster(ctx); err != nil {
		status.State = "Error"
		status.Message = "Failed to reconcile RayCluster"
		return status, err
	}

	// Reconcile the ConfigMap for RayService if needed
	if err := r.ReconcileConfigMap(ctx); err != nil {
		status.State = "Error"
		status.Message = "Failed to reconcile ConfigMap"
		return status, err
	}

	// Reconcile the Secret for RayService if needed
	if err := r.ReconcileSecret(ctx); err != nil {
		status.State = "Error"
		status.Message = "Failed to reconcile Secret"
		return status, err
	}

	// Reconcile the Service for RayService
	if err := r.ReconcileService(ctx); err != nil {
		status.State = "Error"
		status.Message = "Failed to reconcile Service"
		return status, err
	}

	// If all reconciliations succeed, update the status to Running
	status.State = "Running"
	status.Message = "RayService is running successfully"
	return status, nil
}
