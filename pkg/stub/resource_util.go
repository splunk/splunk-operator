package stub

import (
	"fmt"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"strings"
)


const (
	STATEFULSET_TEMPLATE_STR = "splunk-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	STATEFULSET_POD_TEMPLATE_STR = "splunk-instance-%s-%s-%d" // instanceType, identifier, index (ex: 0, 1, 2, ...)
	EXPOSE_SERVICE_TEMPLATE_STR = "splunk-expose-%s-%s-%d" // instanceType, identifier, index
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-headless-%s-%s" // instance type, identifier
	VOLUME_TEMPLATE_STR = "splunk-storage-%s-%s" // instance type, identifier

	SPLUNK_IMAGE = "splunk/splunk:latest"

	PVC_NAME = "storage-fro"
)


func GetClusterComponetUrls(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType, replicas int) string {
	urls := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		urls[i] = fmt.Sprintf(
			"%s.%s.%s.svc.cluster.local",
			fmt.Sprintf(STATEFULSET_POD_TEMPLATE_STR, instanceType.toString(), identifier, i),
			fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, instanceType.toString(), identifier),
			cr.ObjectMeta.Namespace,
		)
	}
	return strings.Join(urls, ",")
}


func CreateSplunkStatefulSet(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType, replicas int, envVariables []corev1.EnvVar) error {

	labels := GetSplunkAppLabels(identifier, map[string]string{"type": instanceType.toString()})
	replicas32 := int32(replicas)

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(STATEFULSET_TEMPLATE_STR, instanceType.toString(), identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, instanceType.toString(), identifier),
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: SPLUNK_IMAGE,
							Name: "splunk",
							Ports: []corev1.ContainerPort{
								{
									Name: "web",
									ContainerPort: 8000,
								},{
									Name: "mgmt",
									ContainerPort: 8089,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name: PVC_NAME,
									MountPath: "/opt/splunk/var",
								},
							},
							Env: envVariables,
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: PVC_NAME,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}

	addOwnerRefToObject(statefulSet, asOwner(cr))

	err := CreateResource(statefulSet)
	if err != nil {
		return err
	}

	err = CreateHeadlessService(cr, identifier, instanceType)
	if err != nil {
		return err
	}

	return nil
}


func CreateHeadlessService(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType) error {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR,  instanceType.toString(), identifier),
			Namespace: cr.Namespace,
			Labels: GetSplunkAppLabels(identifier, map[string]string{"type": HEADLESS_SERVICE.toString()}),
		},
		Spec: corev1.ServiceSpec{
			Selector: GetSplunkAppLabels(identifier, map[string]string{"type": instanceType.toString()}),
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name: "web",
					Port: 8000,
					TargetPort: intstr.FromInt(8000),
				},{
					Name: "mgmt",
					Port: 8089,
					TargetPort: intstr.FromInt(8089),
				},
			},
		},
	}

	addOwnerRefToObject(service, asOwner(cr))

	err := CreateResource(service)
	if err != nil {
		return err
	}

	return nil
}


func CreateExposeService(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType, serviceType ServiceType, i int) error {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(EXPOSE_SERVICE_TEMPLATE_STR, instanceType.toString(), identifier, i),
			Namespace: cr.Namespace,
			Labels: GetSplunkAppLabels(identifier, map[string]string{"type": serviceType.toString()}),
		},
		Spec: corev1.ServiceSpec{
			Selector: GetSplunkAppLabels(identifier, map[string]string{"statefulset.kubernetes.io/pod-name": fmt.Sprintf(STATEFULSET_POD_TEMPLATE_STR, instanceType.toString(), cr.ObjectMeta.Name, i)}),
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Name: "web",
					Port: 8000,
				},{
					Name: "splunkd",
					Port: 8089,
				},
			},
		},
	}

	addOwnerRefToObject(service, asOwner(cr))

	err := CreateResource(service)
	if err != nil {
		return err
	}

	return nil
}


func UpdateSplunkInstance(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType, targetReplicas int) error {
	sts := v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind: "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(STATEFULSET_TEMPLATE_STR, instanceType.toString(), identifier),
			Namespace: cr.Namespace,
		},
	}

	err := sdk.Get(&sts)
	if err != nil {
		return err
	}

	if int(*sts.Spec.Replicas) != targetReplicas {
		targetReplicasInt32 := int32(targetReplicas)
		sts.Spec.Replicas = &targetReplicasInt32
		err = sdk.Update(&sts)
		if err != nil {
			return err
		}
	}

	return nil
}


func UpdateExposeServices(cr *v1alpha1.SplunkInstance, identifier string, instanceType SplunkInstanceType, serviceType ServiceType, targetReplicas int) error {
	serviceList := corev1.ServiceList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Service",
		},
	}
	labelSelector := labels.SelectorFromSet(GetSplunkAppLabels(identifier, map[string]string{"type": serviceType.toString()})).String()
	listOptions := &metav1.ListOptions{LabelSelector: labelSelector}
	err := sdk.List(cr.Namespace, &serviceList, sdk.WithListOptions(listOptions))
	if err != nil {
		return nil
	}

	if len(serviceList.Items) < targetReplicas {
		for i := len(serviceList.Items); i < targetReplicas; i++ {
			CreateExposeService(cr, identifier, instanceType, serviceType, int(i))
		}
	} else if len(serviceList.Items) > targetReplicas {
		for i := len(serviceList.Items); i >= targetReplicas; i-- {
			err := sdk.Delete(&serviceList.Items[i - 1])
			if err != nil {
				return err
			}
		}
	}

	return nil
}


func DeletePersistentVolumeClaims(cr *v1alpha1.SplunkInstance, identifier string) error {
	pvcList := corev1.PersistentVolumeClaimList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "PersistentVolumeClaim",
		},
	}
	labelSelector := labels.SelectorFromSet(GetSplunkAppLabels(identifier, nil)).String()
	listOptions := &metav1.ListOptions{LabelSelector: labelSelector}
	err := sdk.List(cr.Namespace, &pvcList, sdk.WithListOptions(listOptions))
	if err != nil {
		return err
	}

	for _, pvc := range pvcList.Items {
		err = sdk.Delete(&pvc)
		if err != nil {
			return err
		}
	}

	return nil
}


func GetSplunkAppLabels(identifier string, overrides map[string]string) map[string]string {
	labels := map[string]string{
		"app": "splunk",
		"for": identifier,
	}

	if overrides != nil {
		for k, v := range overrides {
			labels[k] = v
		}
	}

	return labels
}


func CreateResource(object sdk.Object) error {
	err := sdk.Create(object)
	if err != nil && !errors.IsAlreadyExists(err) {
		logrus.Errorf("Failed to create object : %v", err)
		return err
	}
	return nil
}


func addOwnerRefToObject(object metav1.Object, reference metav1.OwnerReference) {
	object.SetOwnerReferences(append(object.GetOwnerReferences(), reference))
}


func asOwner(instance *v1alpha1.SplunkInstance) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: instance.APIVersion,
		Kind:       instance.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
		Controller: &trueVar,
	}
}
