// Copyright (c) 2024 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"
	"os"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyKVService reconciles the state for Splunk enterprise KVService.
func ApplyKVService(ctx context.Context, client splcommon.ControllerClient, recorder record.EventRecorder, cr *enterpriseApi.KVService) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyKVService")
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	eventPublisher, _ := newK8EventPublisher(recorder, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "KVService"

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateKVServiceSpec(ctx, client, cr)
	if err != nil {
		recorder.Eventf(cr, corev1.EventTypeWarning, enterpriseApi.EventReasonConfigInvalid, "kvservice spec validation failed %s", err.Error())
		scopedLog.Error(err, "Failed to validate kvservice spec")
		return result, err
	}

	// updates status after function completes
	cr.Status.Replicas = cr.Spec.Replicas

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-kvservice", cr.GetName())

	// ToDo: create or update general config resources
	// _, err = ApplyKVServiceConfig(ctx, client, cr, cr.Spec, SplunkKVService)
	// if err != nil {
	// 	scopedLog.Error(err, "create or update general config failed", "error", err.Error())
	// 	recorder.Eventf(cr, corev1.EventTypeWarning, "ApplySplunkConfig", "create or update general config failed with error %s", err.Error())
	// 	return result, err
	// }

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		err = DeleteKVServiceCR(ctx, client, cr)
		if err != nil {
			recorder.Eventf(cr, corev1.EventTypeWarning, enterpriseApi.EventReasonCRDeletionFailed, "KVService CR deletion failed with error: %s", err.Error())
			return result, err
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkKVService)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)

		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a standard service
	err = splctrl.ApplyService(ctx, client, getStandardK8sServiceForKVServiceDeployment(ctx, cr, &cr.Spec, SplunkKVService))
	if err != nil {
		recorder.Eventf(cr, corev1.EventTypeWarning, enterpriseApi.EventReasonServiceFailed, "create/update headless service failed %s", err.Error())
		return result, err
	}

	// create or update deployment
	deployment, err := getKVServiceDeployment(ctx, client, cr)
	if err != nil {
		recorder.Eventf(cr, corev1.EventTypeWarning, enterpriseApi.EventReasonDeploymentFailed, "KVService deployment failed with error: %s", err.Error())
		return result, err
	}

	phase, err := splctrl.ApplyDeployment(ctx, client, deployment)
	if err != nil {
		recorder.Eventf(cr, corev1.EventTypeWarning, enterpriseApi.EventReasonDeploymentFailed, "kvservice deployment update failed %s", err.Error())

		return result, err
	}

	cr.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		recorder.Event(cr, corev1.EventTypeNormal, enterpriseApi.EventReasonReady,
			fmt.Sprintf("KVService %s is ready with %d replicas", cr.Name, cr.Status.ReadyReplicas))
		// ToDo: If there is a change in connection string, propagate it to other CRs.
		result.Requeue = false
	}

	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// getStandardK8sServiceForKVServiceDeployment eturns a Kubernetes Service object for kvservice resource.
func getStandardK8sServiceForKVServiceDeployment(ctx context.Context, cr splcommon.MetaObject, spec *enterpriseApi.KVServiceSpec, instanceType InstanceType) *corev1.Service {

	var service *corev1.Service

	service = spec.ServiceTemplate.DeepCopy()
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.ObjectMeta.Name = GetSplunkServiceName(instanceType, cr.GetName(), false)
	service.ObjectMeta.Namespace = cr.GetNamespace()

	instanceIdentifier := cr.GetName()
	var partOfIdentifier string

	// ToDo: sgontla: validate the labels are correct
	service.Spec.Selector = getSplunkLabels(instanceIdentifier, instanceType, partOfIdentifier)
	service.Spec.Ports = append(service.Spec.Ports, splcommon.SortServicePorts(getSplunkServicePorts(instanceType))...)

	// ensure labels and annotations are not nil
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	if service.ObjectMeta.Annotations == nil {
		service.ObjectMeta.Annotations = make(map[string]string)
	}
	// append same labels as selector
	for k, v := range service.Spec.Selector {
		service.ObjectMeta.Labels[k] = v
	}
	// append labels and annotations from parent
	splcommon.AppendParentMeta(service.ObjectMeta.GetObjectMeta(), cr.GetObjectMeta())
	service.SetOwnerReferences(append(service.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return service
}

// getKVServiceDeployment returns a Kubernetes Deployment object for Splunk Enterprise KVService instances.
func getKVServiceDeployment(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.KVService) (*appsv1.Deployment, error) {
	spec := &cr.Spec
	instanceType := SplunkKVService
	replicas := spec.Replicas

	ports := splcommon.SortContainerPorts(getSplunkContainerPorts(instanceType))
	annotations := splcommon.GetIstioAnnotations(ports)
	selectLabels := getSplunkLabels(cr.GetName(), instanceType, "")
	affinity := splcommon.AppendPodAntiAffinity(&spec.Affinity, cr.GetName(), instanceType.ToString())

	labels := make(map[string]string)
	for k, v := range selectLabels {
		labels[k] = v
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      GetSplunkDeploymentName(instanceType, cr.GetName()),
	}
	deployment := &appsv1.Deployment{}
	err := client.Get(ctx, namespacedName, deployment)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}

	if k8serrors.IsNotFound(err) {
		deployment = &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetSplunkDeploymentName(instanceType, cr.GetName()),
				Namespace: cr.GetNamespace(),
				Labels:    labels,
			},
		}
	}

	deployment.Spec = appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectLabels,
		},
		Replicas: &replicas,
		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Affinity:                  affinity,
				Tolerations:               spec.Tolerations,
				TopologySpreadConstraints: spec.TopologySpreadConstraints,
				SchedulerName:             spec.SchedulerName,
				ImagePullSecrets:          spec.ImagePullSecrets,
				Containers: []corev1.Container{
					{
						Image:           spec.Image,
						ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
						Name:            "kvservice",
						Ports:           ports,
					},
				},
			},
		},
	}

	if spec.ServiceAccount != "" {
		namespacedName := types.NamespacedName{Namespace: deployment.GetNamespace(), Name: spec.ServiceAccount}
		_, err := splctrl.GetServiceAccount(ctx, client, namespacedName)
		if err == nil {
			deployment.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
		}
	}

	splcommon.AppendParentMeta(deployment.Spec.Template.GetObjectMeta(), cr.GetObjectMeta())

	deploySecret, err := splutil.GetLatestVersionedSecret(ctx, client, cr, cr.GetNamespace(), deployment.GetName())
	if err != nil || deploySecret == nil {
		return deployment, err
	}

	commonSpec := enterpriseApi.CommonSplunkSpec{
		Spec:                         spec.Spec,
		Volumes:                      spec.Volumes,
		Defaults:                     spec.Defaults,
		DefaultsURL:                  spec.DefaultsURL,
		LicenseURL:                   spec.LicenseURL,
		Mock:                         spec.Mock,
		ServiceAccount:               spec.ServiceAccount,
		ExtraEnv:                     spec.ExtraEnv,
		ReadinessInitialDelaySeconds: spec.ReadinessInitialDelaySeconds,
		LivenessInitialDelaySeconds:  spec.LivenessInitialDelaySeconds,
		LivenessProbe:                spec.LivenessProbe,
		ReadinessProbe:               spec.ReadinessProbe,
		ImagePullSecrets:             spec.ImagePullSecrets,
	}

	updateSplunkPodTemplateWithConfig(ctx, client, &deployment.Spec.Template, cr, &commonSpec, instanceType, []corev1.EnvVar{}, deploySecret.GetName())

	kvserviceLivenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/service/liveness",
				// Todo: sgontla: need to read from environment
				Port: intstr.FromInt(defaultKVServicePort),
			},
		},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      1,
		PeriodSeconds:       5,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	kvserviceReadinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/service/readiness",
				// Todo: sgontla: need to read from environment
				Port: intstr.FromInt(defaultKVServicePort),
			},
		},
		InitialDelaySeconds: 30,
		TimeoutSeconds:      1,
		PeriodSeconds:       5,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	for idx := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[idx].Name == "kvservice" {
			deployment.Spec.Template.Spec.Containers[idx].LivenessProbe = kvserviceLivenessProbe
			deployment.Spec.Template.Spec.Containers[idx].ReadinessProbe = kvserviceReadinessProbe
			deployment.Spec.Template.Spec.Containers[idx].StartupProbe = nil
			break
		}
	}

	deployment.SetOwnerReferences(append(deployment.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	return deployment, nil
}

// validateKVServiceSpec checks validity and makes default updates to a KVServiceSpec, and returns error if something is wrong.
func validateKVServiceSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	spec := &cr.Spec
	spec.Image = GetSplunkKVServiceImage(spec.Image)

	defaultResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}

	err := validateLivenessProbe(ctx, cr, spec.LivenessProbe)
	if err != nil {
		return err
	}

	err = validateReadinessProbe(ctx, cr, spec.ReadinessProbe)
	if err != nil {
		return err
	}

	if spec.LivenessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Liveness probe intial delay", spec.LivenessInitialDelaySeconds)
	}

	if spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Readiness probe intial delay", spec.ReadinessInitialDelaySeconds)
	}

	err = validateSplunkGeneralTerms()
	if err != nil {
		return err
	}

	// if not provided, set default values for imagePullSecrets
	err = ValidateKVServiceImagePullSecrets(ctx, c, cr, spec)
	if err != nil {
		return err
	}

	setKVServiceVolumeDefaults(spec)

	return ValidateSpec(&spec.Spec, defaultResources)
}

// GetSplunkKVServiceImage returns the docker image to use for KVService instances.
func GetSplunkKVServiceImage(specImage string) string {
	var name string

	if specImage != "" {
		name = specImage
	} else {
		name = os.Getenv("RELATED_IMAGE_SPLUNK_KVSERVICE")
		if name == "" {
			name = defaultKVServiceImage
		}
	}

	return name
}

// setVolumeDefaults set properties in Volumes to default values
func setKVServiceVolumeDefaults(spec *enterpriseApi.KVServiceSpec) {

	// work-around openapi validation error by ensuring it is not nil
	if spec.Volumes == nil {
		spec.Volumes = []corev1.Volume{}
	}

	for _, v := range spec.Volumes {
		if v.Secret != nil {
			if v.Secret.DefaultMode == nil {
				perm := int32(corev1.SecretVolumeSourceDefaultMode)
				v.Secret.DefaultMode = &perm
			}
			continue
		}

		if v.ConfigMap != nil {
			if v.ConfigMap.DefaultMode == nil {
				perm := int32(corev1.ConfigMapVolumeSourceDefaultMode)
				v.ConfigMap.DefaultMode = &perm
			}
			continue
		}
	}
}

// ValidateImagePullSecrets sets default values for imagePullSecrets if not provided
func ValidateKVServiceImagePullSecrets(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.KVServiceSpec) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ValidateImagePullSecrets").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// If no imagePullSecrets are configured
	var nilImagePullSecrets []corev1.LocalObjectReference
	if len(spec.ImagePullSecrets) == 0 {
		spec.ImagePullSecrets = nilImagePullSecrets
		return nil
	}

	// If configured, validated if the secret/s exist
	for _, secret := range spec.ImagePullSecrets {
		_, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), cr.GetName(), secret.Name)
		if err != nil {
			scopedLog.Error(err, "Couldn't get secret in the imagePullSecrets config", "Secret", secret.Name)
		}
	}

	return nil
}

// helper function to get the list of KVService types in the current namespace
func getKVServiceList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.KVServiceList, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getKVServiceList")

	objectList := enterpriseApi.KVServiceList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "KVService types not found in namespace", "namsespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}
