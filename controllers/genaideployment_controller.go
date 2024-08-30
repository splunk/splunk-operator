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
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	rayv1 "github.com/splunk/splunk-operator/controllers/ray/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
			existingCluster.Spec.WorkerGroupSpecs[i].RayStartParams["num-cpus"] = genAIDeployment.Spec.RayService.WorkerGroup.NumCpus
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
	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("updateRayClusterStatus")

	// Define the desired Deployment object
	desiredDeployment := r.constructSaisServiceDeployment(genAIDeployment)

	// Check if the Deployment already exists
	existingDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: desiredDeployment.Name, Namespace: desiredDeployment.Namespace}, existingDeployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}

		// Create the Deployment if it does not exist
		reqLogger.Info("Creating new Deployment", "Deployment.Namespace", desiredDeployment.Namespace, "Deployment.Name", desiredDeployment.Name)
		if err := r.Create(ctx, desiredDeployment); err != nil {
			return fmt.Errorf("failed to create new Deployment: %w", err)
		}
	} else {
		// Update the existing Deployment if necessary
		if !isEqual(desiredDeployment, existingDeployment) {
			reqLogger.Info("Updating existing Deployment", "Deployment.Namespace", existingDeployment.Namespace, "Deployment.Name", existingDeployment.Name)
			existingDeployment.Spec = desiredDeployment.Spec
			if err := r.Update(ctx, existingDeployment); err != nil {
				return fmt.Errorf("failed to update Deployment: %w", err)
			}
		}
	}

	return nil
}

func (r *GenAIDeploymentReconciler) constructSaisServiceDeployment(genAIDeployment *enterpriseApi.GenAIDeployment) *appsv1.Deployment {
	labels := map[string]string{
		"app":        "sais-service",
		"deployment": genAIDeployment.Name,
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
					Containers: []corev1.Container{
						{
							Name:      "sais-service-container",
							Image:     genAIDeployment.Spec.SaisService.Image,
							Resources: genAIDeployment.Spec.SaisService.Resources,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      genAIDeployment.Spec.SaisService.Volume.Name,
									MountPath: "/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						genAIDeployment.Spec.SaisService.Volume,
					},
					SchedulerName: genAIDeployment.Spec.SaisService.SchedulerName,
					Affinity:      &genAIDeployment.Spec.SaisService.Affinity,
					Tolerations:   genAIDeployment.Spec.SaisService.Tolerations,
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
		// Compare RayStartParams
		if worker.RayStartParams["num-cpus"] != desiredWorker.NumCpus {
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
