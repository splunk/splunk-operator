/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"

	//promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	rayv1 "github.com/splunk/splunk-operator/controllers/ray/v1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GenAIDeploymentReconciler reconciles a GenAIDeployment object
type GenAIDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=genaideployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=genaideployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=genaideployments/finalizers,verbs=update

func (r *GenAIDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("GenAIDeploymentReconciler", req.NamespacedName)

	// Fetch the GenAIDeployment instance
	genAIDeployment := &enterpriseApi.GenAIDeployment{}
	err := r.Client.Get(ctx, req.NamespacedName, genAIDeployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			reqLogger.Error(err, "Failed to get GenAIDeployment")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle RayCluster creation/update
	if genAIDeployment.Spec.RayService.Enabled {
		rayCluster := &rayv1.RayCluster{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: req.Name + "-raycluster", Namespace: req.Namespace}, rayCluster)
		if err != nil {
			// Create RayCluster if not found
			newRayCluster := r.constructRayCluster(ctx, genAIDeployment)
			if err := r.Client.Create(ctx, newRayCluster); err != nil {
				reqLogger.Error(err, "Failed to create RayCluster")
				return ctrl.Result{}, err
			}
		} else {
			// Update existing RayCluster if necessary
			updatedRayCluster := r.updateRayCluster(ctx, rayCluster, genAIDeployment)
			if err := r.Client.Update(ctx, updatedRayCluster); err != nil {
				reqLogger.Error(err, "Failed to update RayCluster")
				return ctrl.Result{}, err
			}
		}

		// Update Status with RayCluster information
		r.updateRayClusterStatus(ctx, genAIDeployment, rayCluster)
	}

	// Reconcile SaisService Deployment
	if err := r.reconcileSaisServiceDeployment(ctx, genAIDeployment); err != nil {
		reqLogger.Error(err, "Failed to reconcile SaisService Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile VectorDb StatefulSet
	if err := r.reconcileVectorDbStatefulSet(ctx, genAIDeployment); err != nil {
		reqLogger.Error(err, "Failed to reconcile VectorDb StatefulSet")
		return ctrl.Result{}, err
	}

	// Reconcile PrometheusRule if Prometheus Operator is installed
	/*if err := r.reconcilePrometheusRule(ctx, genAIDeployment); err != nil {
		reqLogger.Error(err, "Failed to reconcile PrometheusRule")
		return ctrl.Result{}, err
	}
	*/

	// Update Status
	if err := r.updateGenAIDeploymentStatus(ctx, genAIDeployment); err != nil {
		reqLogger.Error(err, "Failed to update GenAIDeployment status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GenAIDeploymentReconciler) constructRayCluster(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) *rayv1.RayCluster {
	// Create RayCluster object based on GenAIDeployment spec
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      genAIDeployment.Name + "-raycluster",
			Namespace: genAIDeployment.Namespace,
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{
					"num-cpus": genAIDeployment.Spec.RayService.HeadGroup.NumCpus,
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:      "ray-head",
								Image:     genAIDeployment.Spec.RayService.Image,
								Resources: genAIDeployment.Spec.RayService.HeadGroup.Resources,
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					GroupName: "ray-worker",
					Replicas:  &genAIDeployment.Spec.RayService.WorkerGroup.Replicas,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "ray-worker",
									Image:     genAIDeployment.Spec.RayService.Image,
									Resources: genAIDeployment.Spec.RayService.WorkerGroup.Resources,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *GenAIDeploymentReconciler) updateRayClusterStatus(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment, rayCluster *rayv1.RayCluster) {
	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("updateRayClusterStatus")

	// Fetch RayCluster status and update GenAIDeployment status
	genAIDeployment.Status.RayClusterStatus = enterpriseApi.RayClusterStatus{
		ClusterName: rayCluster.Name,
		State:       string(rayCluster.Status.State),
		Conditions:  rayCluster.Status.Conditions,
	}
	err := r.Client.Status().Update(context.Background(), genAIDeployment)
	if err != nil {
		reqLogger.Error(err, "Failed to update GenAIDeployment status")
	}
}

func (r *GenAIDeploymentReconciler) updateRayCluster(ctx context.Context, existingCluster *rayv1.RayCluster, genAIDeployment *enterpriseApi.GenAIDeployment) *rayv1.RayCluster {
	// Check and update the HeadGroupSpec
	if !isRayHeadGroupEqual(existingCluster.Spec.HeadGroupSpec, genAIDeployment.Spec.RayService.HeadGroup) {
		existingCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources = genAIDeployment.Spec.RayService.HeadGroup.Resources
		existingCluster.Spec.HeadGroupSpec.RayStartParams["num-cpus"] = genAIDeployment.Spec.RayService.HeadGroup.NumCpus
	}

	// Check and update the WorkerGroupSpec
	if !isRayWorkerGroupEqual(existingCluster.Spec.WorkerGroupSpecs, genAIDeployment.Spec.RayService.WorkerGroup) {
		for i := range existingCluster.Spec.WorkerGroupSpecs {
			existingCluster.Spec.WorkerGroupSpecs[i].Template.Spec.Containers[0].Resources = genAIDeployment.Spec.RayService.WorkerGroup.Resources
		}
	}

	// Update general configurations if they have changed
	existingCluster.Spec.RayVersion = genAIDeployment.Spec.RayService.Image
	existingCluster.Spec.WorkerGroupSpecs[0].Replicas = &genAIDeployment.Spec.RayService.WorkerGroup.Replicas

	return existingCluster
}

func (r *GenAIDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterpriseApi.GenAIDeployment{}).
		Owns(&rayv1.RayCluster{}).
		Complete(r)
}

func (r *GenAIDeploymentReconciler) reconcileSaisServiceDeployment(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Define the desired Deployment object for the SaisService
	desiredDeployment := r.constructSaisServiceDeployment(genAIDeployment)

	// Check if the Deployment already exists
	existingDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: desiredDeployment.Name, Namespace: desiredDeployment.Namespace}, existingDeployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the Deployment if it does not exist
		log.Info("Creating new SaisService Deployment", "Deployment.Namespace", desiredDeployment.Namespace, "Deployment.Name", desiredDeployment.Name)
		if err := r.Create(ctx, desiredDeployment); err != nil {
			return fmt.Errorf("failed to create new SaisService Deployment: %w", err)
		}
	} else {
		// Update the existing Deployment if necessary
		if !isSaisServiceDeploymentEqual(desiredDeployment, existingDeployment) {
			log.Info("Updating existing SaisService Deployment", "Deployment.Namespace", existingDeployment.Namespace, "Deployment.Name", existingDeployment.Name)
			existingDeployment.Spec = desiredDeployment.Spec
			if err := r.Update(ctx, existingDeployment); err != nil {
				return fmt.Errorf("failed to update SaisService Deployment: %w", err)
			}
		}
	}

	// Reconcile SaisService Service
	if err := r.reconcileSaisService(ctx, genAIDeployment); err != nil {
		log.Error(err, "Failed to reconcile SaisService")
		return err
	}

	return nil
}

func isSaisServiceDeploymentEqual(desiredDeployment, existingDeployment *appsv1.Deployment) bool {
	// Compare Replicas
	if *desiredDeployment.Spec.Replicas != *existingDeployment.Spec.Replicas {
		return false
	}

	// Compare Image
	if desiredDeployment.Spec.Template.Spec.Containers[0].Image != existingDeployment.Spec.Template.Spec.Containers[0].Image {
		return false
	}

	// Compare Resources
	if !equalResourceRequirements(desiredDeployment.Spec.Template.Spec.Containers[0].Resources, existingDeployment.Spec.Template.Spec.Containers[0].Resources) {
		return false
	}

	// Compare Environment Variables
	if !reflect.DeepEqual(desiredDeployment.Spec.Template.Spec.Containers[0].Env, existingDeployment.Spec.Template.Spec.Containers[0].Env) {
		return false
	}

	// Compare Node Selector
	if !reflect.DeepEqual(desiredDeployment.Spec.Template.Spec.NodeSelector, existingDeployment.Spec.Template.Spec.NodeSelector) {
		return false
	}

	// Compare Affinity Rules
	if !reflect.DeepEqual(desiredDeployment.Spec.Template.Spec.Affinity, existingDeployment.Spec.Template.Spec.Affinity) {
		return false
	}

	// Compare Tolerations
	if !reflect.DeepEqual(desiredDeployment.Spec.Template.Spec.Tolerations, existingDeployment.Spec.Template.Spec.Tolerations) {
		return false
	}

	// Compare Topology Spread Constraints
	if !reflect.DeepEqual(desiredDeployment.Spec.Template.Spec.TopologySpreadConstraints, existingDeployment.Spec.Template.Spec.TopologySpreadConstraints) {
		return false
	}

	// If all checks pass, the desired and existing deployments are considered equal
	return true
}

func (r *GenAIDeploymentReconciler) reconcileSaisService(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Define the desired Service object for the SaisService
	desiredService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sais-service", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":        "sais-service",
				"deployment": genAIDeployment.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8080, // Example port for SaisService, replace with actual port if different
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	// Check if the Service already exists
	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: desiredService.Name, Namespace: desiredService.Namespace}, existingService)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the Service if it does not exist
		log.Info("Creating new SaisService Service", "Service.Namespace", desiredService.Namespace, "Service.Name", desiredService.Name)
		if err := r.Create(ctx, desiredService); err != nil {
			return fmt.Errorf("failed to create new SaisService Service: %w", err)
		}
	} else {
		log.Info("SaisService Service already exists", "Service.Namespace", existingService.Namespace, "Service.Name", existingService.Name)
	}

	return nil
}

func (r *GenAIDeploymentReconciler) constructSaisServiceDeployment(genAIDeployment *enterpriseApi.GenAIDeployment) *appsv1.Deployment {
	labels := map[string]string{
		"app":        "sais-service",
		"deployment": genAIDeployment.Name,
	}

	// Define node selector and tolerations for GPU support
	nodeSelector := map[string]string{}
	tolerations := []corev1.Toleration{}

	if genAIDeployment.Spec.RequireGPU {
		// Assuming nodes with GPU support are labeled as "kubernetes.io/gpu: true"
		nodeSelector["kubernetes.io/gpu"] = "true"

		// Add a toleration for GPU nodes if needed
		tolerations = append(tolerations, corev1.Toleration{
			Key:      "nvidia.com/gpu",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		})
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sais-service", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &genAIDeployment.Spec.SaisService.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
					Tolerations:  tolerations,
					Containers: []corev1.Container{
						{
							Name:  "sais-container",
							Image: genAIDeployment.Spec.SaisService.Image,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							Env: []corev1.EnvVar{
								{Name: "IAC_URL", Value: "auth.playground.scs.splunk.com"},
								{Name: "API_GATEWAY_URL", Value: "api.playground.scs.splunk.com"},
								{Name: "PLATFORM_URL", Value: "ml-platform-cyclops.dev.svc.splunk8s.io"},
								{Name: "TELEMETRY_URL", Value: "https://telemetry-splkmobile.kube-bridger"},
								{Name: "TELEMETRY_ENV", Value: "local"},
								{Name: "TELEMETRY_REGION", Value: "region-iad10"},
								{Name: "ENABLE_AUTHZ", Value: "false"},
								{Name: "AUTH_PROVIDER", Value: "scp"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      genAIDeployment.Spec.SaisService.Volume.Name,
									MountPath: "/data",
								},
							},
						},
					},
					Affinity:                  &genAIDeployment.Spec.SaisService.Affinity,
					TopologySpreadConstraints: genAIDeployment.Spec.SaisService.TopologySpreadConstraints,
				},
			},
		},
	}

	// Set the owner reference to enable garbage collection
	ctrl.SetControllerReference(genAIDeployment, deployment, r.Scheme)
	return deployment
}

func isEqual(desired, existing *appsv1.Deployment) bool {
	// Compare important fields for determining if an update is necessary
	// This is a simplified example; you may need a more thorough comparison
	return desired.Spec.Replicas == existing.Spec.Replicas &&
		desired.Spec.Template.Spec.Containers[0].Image == existing.Spec.Template.Spec.Containers[0].Image
}

func (r *GenAIDeploymentReconciler) reconcileVectorDbStatefulSet(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Reconcile Persistent Volume Claims based on the storage spec
	pvc, err := r.reconcileVectorDbPVC(ctx, genAIDeployment)
	if err != nil {
		log.Error(err, "Failed to reconcile VectorDb PersistentVolumeClaim")
		return err
	}

	// Define the desired StatefulSet object for the VectorDb service
	desiredStatefulSet := r.constructVectorDbStatefulSet(genAIDeployment, pvc)

	// Check if the StatefulSet already exists
	existingStatefulSet := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKey{Name: desiredStatefulSet.Name, Namespace: desiredStatefulSet.Namespace}, existingStatefulSet)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the StatefulSet if it does not exist
		log.Info("Creating new VectorDb StatefulSet", "StatefulSet.Namespace", desiredStatefulSet.Namespace, "StatefulSet.Name", desiredStatefulSet.Name)
		if err := r.Create(ctx, desiredStatefulSet); err != nil {
			return fmt.Errorf("failed to create new VectorDb StatefulSet: %w", err)
		}
	} else {
		// Update the existing StatefulSet if necessary
		if !isVectorDbStatefulSetEqual(desiredStatefulSet, existingStatefulSet) {
			log.Info("Updating existing VectorDb StatefulSet", "StatefulSet.Namespace", existingStatefulSet.Namespace, "StatefulSet.Name", existingStatefulSet.Name)
			existingStatefulSet.Spec = desiredStatefulSet.Spec
			if err := r.Update(ctx, existingStatefulSet); err != nil {
				return fmt.Errorf("failed to update VectorDb StatefulSet: %w", err)
			}
		}
	}

	// Reconcile VectorDb Service
	if err := r.reconcileVectorDbService(ctx, genAIDeployment); err != nil {
		log.Error(err, "Failed to reconcile VectorDb Service")
		return err
	}

	return nil
}

func (r *GenAIDeploymentReconciler) reconcileVectorDbService(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Define the desired Service object for the VectorDb service
	desiredService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-vectordb-service", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":        "vectordb-service",
				"deployment": genAIDeployment.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       5432, //
					TargetPort: intstr.FromInt(5432),
				},
			},
			ClusterIP: corev1.ClusterIPNone, // StatefulSet requires ClusterIP=None for headless service
		},
	}

	// Check if the Service already exists
	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: desiredService.Name, Namespace: desiredService.Namespace}, existingService)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the Service if it does not exist
		log.Info("Creating new VectorDb Service", "Service.Namespace", desiredService.Namespace, "Service.Name", desiredService.Name)
		if err := r.Create(ctx, desiredService); err != nil {
			return fmt.Errorf("failed to create new VectorDb Service: %w", err)
		}
	} else {
		log.Info("VectorDb Service already exists", "Service.Namespace", existingService.Namespace, "Service.Name", existingService.Name)
	}

	return nil
}

func (r *GenAIDeploymentReconciler) reconcileVectorDbPVC(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) (*corev1.PersistentVolumeClaim, error) {
	log := log.FromContext(ctx)
	storageSpec := genAIDeployment.Spec.VectorDbService.Storage

	// Use ephemeral storage if specified
	if storageSpec.EphemeralStorage {
		log.Info("Using ephemeral storage (emptyDir) for VectorDb")
		return nil, nil
	}

	// Define the PVC object
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-vectordb-pvc", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSpec.StorageCapacity),
				},
			},
			StorageClassName: &storageSpec.StorageClassName,
		},
	}

	// Check if the PVC already exists
	existingPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Name: pvc.Name, Namespace: pvc.Namespace}, existingPVC)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		// Create the PVC if it does not exist
		log.Info("Creating new PersistentVolumeClaim", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
		if err := r.Create(ctx, pvc); err != nil {
			return nil, fmt.Errorf("failed to create new PersistentVolumeClaim: %w", err)
		}
	} else {
		log.Info("PersistentVolumeClaim already exists", "PVC.Namespace", existingPVC.Namespace, "PVC.Name", existingPVC.Name)
		return existingPVC, nil
	}

	return pvc, nil
}

func (r *GenAIDeploymentReconciler) constructVectorDbStatefulSet(genAIDeployment *enterpriseApi.GenAIDeployment, pvc *corev1.PersistentVolumeClaim) *appsv1.StatefulSet {
	labels := map[string]string{
		"app":        "vectordb-service",
		"deployment": genAIDeployment.Name,
	}

	var volumeMounts []corev1.VolumeMount
	var volumeClaims []corev1.PersistentVolumeClaim
	var volumes []corev1.Volume

	if pvc == nil {
		// Use ephemeral storage
		volumes = append(volumes, corev1.Volume{
			Name: "vectordb-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "vectordb-storage",
			MountPath: "/data",
		})
	} else {
		// Use persistent storage
		volumeClaims = append(volumeClaims, *pvc)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      pvc.Name,
			MountPath: "/data",
		})
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-vectordb-service", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: fmt.Sprintf("%s-vectordb-service", genAIDeployment.Name),
			Replicas:    &genAIDeployment.Spec.VectorDbService.Replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:         "vectordb-container",
							Image:        genAIDeployment.Spec.VectorDbService.Image,
							Resources:    genAIDeployment.Spec.VectorDbService.Resources,
							VolumeMounts: volumeMounts,
						},
					},
					Affinity:                  &genAIDeployment.Spec.VectorDbService.Affinity,
					Tolerations:               genAIDeployment.Spec.VectorDbService.Tolerations,
					TopologySpreadConstraints: genAIDeployment.Spec.VectorDbService.TopologySpreadConstraints,
					Volumes:                   volumes,
				},
			},
			VolumeClaimTemplates: volumeClaims,
		},
	}

	// Set the owner reference to enable garbage collection
	ctrl.SetControllerReference(genAIDeployment, statefulSet, r.Scheme)
	return statefulSet
}

func isVectorDbStatefulSetEqual(desired, existing *appsv1.StatefulSet) bool {
	// Compare replicas, resources, and other fields as necessary
	return desired.Spec.Replicas != nil && existing.Spec.Replicas != nil &&
		*desired.Spec.Replicas == *existing.Spec.Replicas &&
		desired.Spec.Template.Spec.Containers[0].Image == existing.Spec.Template.Spec.Containers[0].Image &&
		equalResourceRequirements(desired.Spec.Template.Spec.Containers[0].Resources, existing.Spec.Template.Spec.Containers[0].Resources)
}

func getDeploymentStatus(deployment *appsv1.Deployment) (string, string) {
	if len(deployment.Status.Conditions) == 0 {
		return "Unknown", "No conditions reported"
	}
	condition := deployment.Status.Conditions[len(deployment.Status.Conditions)-1]
	return string(condition.Type), condition.Message
}

func getRayClusterState(rayCluster *rayv1.RayCluster) string {
	if len(rayCluster.Status.Conditions) == 0 {
		return "Unknown"
	}
	// Return the state based on the latest condition
	latestCondition := rayCluster.Status.Conditions[len(rayCluster.Status.Conditions)-1]
	if latestCondition.Status == metav1.ConditionTrue {
		return string(latestCondition.Type)
	}
	return "NotReady"
}

func isRayHeadGroupEqual(existingHead rayv1.HeadGroupSpec, desiredHead enterpriseApi.HeadGroup) bool {
	// Compare resources
	if !equalResourceRequirements(existingHead.Template.Spec.Containers[0].Resources, desiredHead.Resources) {
		return false
	}
	// Compare RayStartParams
	if existingHead.RayStartParams["num-cpus"] != desiredHead.NumCpus {
		return false
	}
	return true
}

func isRayWorkerGroupEqual(existingWorkers []rayv1.WorkerGroupSpec, desiredWorker enterpriseApi.WorkerGroup) bool {
	for _, worker := range existingWorkers {
		// Compare resources
		if !equalResourceRequirements(worker.Template.Spec.Containers[0].Resources, desiredWorker.Resources) {
			return false
		}
	}
	return true
}

func equalResourceRequirements(a, b corev1.ResourceRequirements) bool {
	return a.Limits.Cpu().Equal(*b.Limits.Cpu()) &&
		a.Limits.Memory().Equal(*b.Limits.Memory()) &&
		a.Requests.Cpu().Equal(*b.Requests.Cpu()) &&
		a.Requests.Memory().Equal(*b.Requests.Memory())
}

func (r *GenAIDeploymentReconciler) updateGenAIDeploymentStatus(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Fetch the SaisService Deployment status
	saisServiceDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-sais-service", genAIDeployment.Name), Namespace: genAIDeployment.Namespace}, saisServiceDeployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		genAIDeployment.Status.SaisServiceStatus = enterpriseApi.SaisServiceStatus{
			Name:     fmt.Sprintf("%s-sais-service", genAIDeployment.Name),
			Replicas: 0,
			Status:   "NotFound",
			Message:  "SaisService Deployment not found",
		}
	} else {
		// Update the status based on the Deployment's current state
		status, message := getDeploymentStatus(saisServiceDeployment)
		genAIDeployment.Status.SaisServiceStatus = enterpriseApi.SaisServiceStatus{
			Name:     saisServiceDeployment.Name,
			Replicas: saisServiceDeployment.Status.ReadyReplicas,
			Status:   status,
			Message:  message,
		}
	}

	// Fetch the VectorDb StatefulSet status
	vectorDbStatefulSet := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-vectordb-service", genAIDeployment.Name), Namespace: genAIDeployment.Namespace}, vectorDbStatefulSet)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		genAIDeployment.Status.VectorDbStatus = enterpriseApi.VectorDbStatus{
			Enabled: genAIDeployment.Spec.VectorDbService.Enabled,
			Status:  "NotFound",
			Message: "VectorDb StatefulSet not found",
		}
	} else {
		// Update the status based on the StatefulSet's current state
		statefulSetStatus, statefulSetMessage := getStatefulSetStatus(vectorDbStatefulSet)
		genAIDeployment.Status.VectorDbStatus = enterpriseApi.VectorDbStatus{
			Enabled: genAIDeployment.Spec.VectorDbService.Enabled,
			Status:  statefulSetStatus,
			Message: statefulSetMessage,
		}
	}

	// Fetch the RayCluster status
	rayCluster := &rayv1.RayCluster{}
	err = r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-ray-cluster", genAIDeployment.Name), Namespace: genAIDeployment.Namespace}, rayCluster)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		genAIDeployment.Status.RayClusterStatus = enterpriseApi.RayClusterStatus{
			ClusterName: fmt.Sprintf("%s-ray-cluster", genAIDeployment.Name),
			State:       "NotFound",
			Conditions:  []metav1.Condition{},
		}
	} else {
		// Update RayCluster status with its current conditions
		genAIDeployment.Status.RayClusterStatus = enterpriseApi.RayClusterStatus{
			ClusterName: rayCluster.Name,
			State:       getRayClusterState(rayCluster),
			Conditions:  rayCluster.Status.Conditions,
		}
	}

	// Update the GenAIDeployment status
	if err := r.Status().Update(ctx, genAIDeployment); err != nil {
		log.Error(err, "Failed to update GenAIDeployment status")
		return err
	}

	return nil
}

func getStatefulSetStatus(statefulSet *appsv1.StatefulSet) (string, string) {
	if len(statefulSet.Status.Conditions) == 0 {
		return "Unknown", "No conditions reported"
	}
	condition := statefulSet.Status.Conditions[len(statefulSet.Status.Conditions)-1]
	return string(condition.Type), condition.Message
}

/*
func (r *GenAIDeploymentReconciler) reconcilePrometheusRule(ctx context.Context, genAIDeployment *enterpriseApi.GenAIDeployment) error {
	log := log.FromContext(ctx)

	// Check if Prometheus Operator is enabled
	if !genAIDeployment.Spec.PrometheusOperator {
		log.Info("Prometheus Operator is not installed. Skipping PrometheusRule creation.")
		return nil
	}

	// Fetch the ConfigMap containing Prometheus rules
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: genAIDeployment.Spec.PrometheusRuleConfig, Namespace: genAIDeployment.Namespace}, configMap)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get Prometheus rules ConfigMap: %w", err)
		}
		log.Info("ConfigMap for Prometheus rules not found, skipping reconciliation.")
		return nil
	}

	// Extract rules from the ConfigMap
	ruleData, ok := configMap.Data["prometheus_rules.yaml"]
	if !ok {
		log.Info("No Prometheus rules found in ConfigMap, skipping reconciliation.")
		return nil
	}

	// Parse the Prometheus rules from the ConfigMap data
	var ruleSpec promv1.PrometheusRuleSpec
	err = yaml.Unmarshal([]byte(ruleData), &ruleSpec)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Prometheus rules from ConfigMap: %w", err)
	}

	// Define the desired PrometheusRule object
	desiredRule := &promv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-prometheus-rule", genAIDeployment.Name),
			Namespace: genAIDeployment.Namespace,
		},
		Spec: ruleSpec,
	}

	// Check if the PrometheusRule already exists
	existingRule := &promv1.PrometheusRule{}
	err = r.Get(ctx, client.ObjectKey{Name: desiredRule.Name, Namespace: desiredRule.Namespace}, existingRule)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the PrometheusRule if it does not exist
		log.Info("Creating new PrometheusRule", "PrometheusRule.Namespace", desiredRule.Namespace, "PrometheusRule.Name", desiredRule.Name)
		if err := r.Create(ctx, desiredRule); err != nil {
			return fmt.Errorf("failed to create new PrometheusRule: %w", err)
		}
	} else {
		// Update the existing PrometheusRule if necessary
		if !reflect.DeepEqual(desiredRule.Spec, existingRule.Spec) {
			log.Info("Updating existing PrometheusRule", "PrometheusRule.Namespace", existingRule.Namespace, "PrometheusRule.Name", existingRule.Name)
			existingRule.Spec = desiredRule.Spec
			if err := r.Update(ctx, existingRule); err != nil {
				return fmt.Errorf("failed to update PrometheusRule: %w", err)
			}
		}
	}

	return nil
}
*/
