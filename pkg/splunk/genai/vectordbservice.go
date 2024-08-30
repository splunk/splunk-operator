package genai

import (
	"context"
	"fmt"
	"reflect"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VectorDbReconciler is an interface for reconciling the VectorDbService and its associated resources.
type VectorDbReconciler interface {
	Reconcile(ctx context.Context) (enterpriseApi.VectorDbStatus, error)
	ReconcileSecret(ctx context.Context) error
	ReconcileConfigMap(ctx context.Context) error
	ReconcileStatefulSet(ctx context.Context) error
	ReconcileServices(ctx context.Context) error
}

// vectorDbReconcilerImpl is the concrete implementation of VectorDbReconciler.
type vectorDbReconcilerImpl struct {
	client.Client
	genAIDeployment *enterpriseApi.GenAIDeployment
	eventRecorder   *splutil.K8EventPublisher
}

// NewVectorDbReconciler creates a new instance of VectorDbReconciler.
func NewVectorDbReconciler(c client.Client, genAIDeployment *enterpriseApi.GenAIDeployment, eventRecorder *splutil.K8EventPublisher) VectorDbReconciler {
	return &vectorDbReconcilerImpl{
		Client:          c,
		genAIDeployment: genAIDeployment,
		eventRecorder:   eventRecorder,
	}
}

// Reconcile manages the complete reconciliation logic for the VectorDbService and returns its status.
func (r *vectorDbReconcilerImpl) Reconcile(ctx context.Context) (enterpriseApi.VectorDbStatus, error) {
	status := enterpriseApi.VectorDbStatus{}

	// Reconcile the Secret for Weaviate
	if err := r.ReconcileSecret(ctx); err != nil {
		status.Status = "Error"
		status.Message = "Failed to reconcile Secret"
		return status, err
	}

	// Reconcile the ConfigMap for Weaviate
	if err := r.ReconcileConfigMap(ctx); err != nil {
		status.Status = "Error"
		status.Message = "Failed to reconcile ConfigMap"
		return status, err
	}

	// Reconcile the StatefulSet for Weaviate
	if err := r.ReconcileStatefulSet(ctx); err != nil {
		status.Status = "Error"
		status.Message = "Failed to reconcile StatefulSet"
		return status, err
	}

	// Reconcile the Services for Weaviate
	if err := r.ReconcileServices(ctx); err != nil {
		status.Status = "Error"
		status.Message = "Failed to reconcile Services"
		return status, err
	}

	// If all reconciliations succeed, update the status to Running
	status.Status = "Running"
	status.Message = "Weaviate is running successfully"
	return status, nil
}

func (r *vectorDbReconcilerImpl) ReconcileSecret(ctx context.Context) error {
	// Get the SecretRef from the VectorDbService spec
	secretName := r.genAIDeployment.Spec.VectorDbService.SecretRef
	if secretName.Name == "" {
		err := fmt.Errorf("SecretRef is not specified in the VectorDbService spec")
		r.eventRecorder.Warning(ctx, "MissingSecretRef", err.Error())
		return err
	}

	// Fetch the existing Secret using the SecretRef
	existingSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: secretName.Name, Namespace: r.genAIDeployment.Namespace}, existingSecret)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			r.eventRecorder.Warning(ctx, "GetSecretFailed", fmt.Sprintf("Failed to get existing Secret %s: %v", secretName, err))
			return fmt.Errorf("failed to get existing Secret: %w", err)
		}
		// Secret does not exist, return an error indicating it is required
		err := fmt.Errorf("secret '%s' referenced by SecretRef does not exist", secretName)
		r.eventRecorder.Warning(ctx, "SecretNotFound", err.Error())
		return err
	}

	// Ensure that the Secret has the expected data keys
	requiredKeys := []string{"username", "password"}
	for _, key := range requiredKeys {
		if _, exists := existingSecret.Data[key]; !exists {
			err := fmt.Errorf("secret '%s' is missing required key: %s", secretName, key)
			r.eventRecorder.Warning(ctx, "SecretInvalid", err.Error())
			return err
		}
	}

	// Add OwnerReference to the Secret if not already set
	ownerRef := metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment"))
	if !isOwnerReferenceSet(existingSecret.OwnerReferences, ownerRef) {
		existingSecret.OwnerReferences = append(existingSecret.OwnerReferences, *ownerRef)
		if err := r.Update(ctx, existingSecret); err != nil {
			r.eventRecorder.Warning(ctx, "UpdateSecretFailed", fmt.Sprintf("Failed to update Secret with OwnerReference: %v", err))
			return fmt.Errorf("failed to update Secret with OwnerReference: %w", err)
		}
		r.eventRecorder.Normal(ctx, "UpdatedSecret", fmt.Sprintf("Successfully updated Secret %s with OwnerReference", secretName))
	}

	return nil
}

// isOwnerReferenceSet checks if the owner reference is already set
func isOwnerReferenceSet(ownerRefs []metav1.OwnerReference, ownerRef *metav1.OwnerReference) bool {
	for _, ref := range ownerRefs {
		if ref.UID == ownerRef.UID {
			return true
		}
	}
	return false
}

func (r *vectorDbReconcilerImpl) ReconcileConfigMap(ctx context.Context) error {
	desiredConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "weaviate-config",
			Namespace: r.genAIDeployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "weaviate",
				"app.kubernetes.io/managed-by": "Helm",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Data: map[string]string{
			"conf.yaml": `
authentication:
  anonymous_access:
    enabled: true
  oidc:
    enabled: false
authorization:
  admin_list:
    enabled: false
query_defaults:
  limit: 100
debug: false
`,
		},
	}

	// Fetch the existing ConfigMap
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: desiredConfigMap.Name, Namespace: desiredConfigMap.Namespace}, existingConfigMap)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the ConfigMap if it does not exist
		if err := r.Create(ctx, desiredConfigMap); err != nil {
			r.eventRecorder.Warning(ctx, "CreateConfigMapFailed", fmt.Sprintf("Failed to create ConfigMap: %v", err))
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
		r.eventRecorder.Normal(ctx, "CreatedConfigMap", fmt.Sprintf("Successfully created ConfigMap %s", desiredConfigMap.Name))
	}

	// Compare the existing ConfigMap data with the desired data
	if !reflect.DeepEqual(existingConfigMap.Data, desiredConfigMap.Data) {
		// Update the existing ConfigMap's data
		existingConfigMap.Data = desiredConfigMap.Data

		// Update the ConfigMap in the cluster
		if err := r.Update(ctx, existingConfigMap); err != nil {
			r.eventRecorder.Warning(ctx, "UpdateConfigMapFailed", fmt.Sprintf("Failed to update ConfigMap: %v", err))
			return fmt.Errorf("failed to update ConfigMap: %w", err)
		}
		r.eventRecorder.Normal(ctx, "UpdatedConfigMap", fmt.Sprintf("Successfully updated ConfigMap %s", existingConfigMap.Name))
	}

	return nil
}

func (r *vectorDbReconcilerImpl) ReconcileStatefulSet(ctx context.Context) error {
	labels := map[string]string{
		"app":        "weaviate",
		"deployment": r.genAIDeployment.Name,
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "weaviate",
			Namespace: r.genAIDeployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "weaviate",
				"app.kubernetes.io/managed-by": "Helm",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &r.genAIDeployment.Spec.VectorDbService.Replicas,
			ServiceName: "weaviate-headless",
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "weaviate",
							Image: "cr.weaviate.io/semitechnologies/weaviate:1.26.1",
							Env: []corev1.EnvVar{
								{Name: "CLUSTER_BASIC_AUTH_USERNAME", ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "weaviate-cluster-api-basic-auth"},
										Key:                  "username",
									}}},
								{Name: "CLUSTER_BASIC_AUTH_PASSWORD", ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "weaviate-cluster-api-basic-auth"},
										Key:                  "password",
									}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "weaviate-config",
									MountPath: "/weaviate-config",
								},
								{
									Name:      "weaviate-data",
									MountPath: "/var/lib/weaviate",
								},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{Name: "grpc", ContainerPort: 50051, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			},
		},
	}

	existingStatefulSet := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Name: statefulSet.Name, Namespace: statefulSet.Namespace}, existingStatefulSet)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		if err := r.Create(ctx, statefulSet); err != nil {
			r.eventRecorder.Warning(ctx, "CreateStatefulSetFailed", fmt.Errorf("failed to create StatefulSet: %w", err).Error())
			return fmt.Errorf("failed to create StatefulSet: %w", err)
		}
	} else if !reflect.DeepEqual(statefulSet.Spec, existingStatefulSet.Spec) {
		existingStatefulSet.Spec = statefulSet.Spec
		if err := r.Update(ctx, existingStatefulSet); err != nil {
			return fmt.Errorf("failed to update StatefulSet: %w", err)
		}
	}
	r.eventRecorder.Normal(ctx, "ReconcileStatefulSet", "Successfully reconciled StatefulSet")
	return nil
}

func (r *vectorDbReconcilerImpl) ReconcileServices(ctx context.Context) error {
	// Define the desired services: weaviate, weaviate-grpc, and weaviate-headless
	services := []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "weaviate",
				Namespace: r.genAIDeployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "weaviate",
					"app.kubernetes.io/managed-by": "Helm",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Selector: map[string]string{
					"app": "weaviate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Port:       80,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "weaviate-grpc",
				Namespace: r.genAIDeployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "weaviate",
					"app.kubernetes.io/managed-by": "Helm",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Selector: map[string]string{
					"app": "weaviate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "grpc",
						Port:       50051,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromInt(50051),
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "weaviate-headless",
				Namespace: r.genAIDeployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "weaviate",
					"app.kubernetes.io/managed-by": "Helm",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
				},
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "None", // Headless service
				Selector: map[string]string{
					"app": "weaviate",
				},
				Ports: []corev1.ServicePort{
					{
						Protocol:   corev1.ProtocolTCP,
						Port:       80,
						TargetPort: intstr.FromInt(7000),
					},
				},
				PublishNotReadyAddresses: true,
			},
		},
	}

	// Loop through each service and reconcile
	for _, service := range services {
		existingService := &corev1.Service{}
		err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, existingService)
		if err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to get existing Service %s: %w", service.Name, err)
			}
			// Service does not exist, so create it
			if err := r.Create(ctx, service); err != nil {
				return fmt.Errorf("failed to create Service %s: %w", service.Name, err)
			}
			r.eventRecorder.Normal(ctx, "CreatedService", fmt.Sprintf("Successfully created Service %s", service.Name))
		} else {
			// If the service exists, do nothing
		}
	}

	r.eventRecorder.Normal(ctx, "ReconcileServices", "Successfully reconciled Services")

	return nil
}
