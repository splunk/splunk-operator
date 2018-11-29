package resources


import (
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/spark"
	"operator/splunk-operator/pkg/stub/splunk"
)


func CreateSplunkStatefulSet(cr *v1alpha1.SplunkInstance, instanceType splunk.SplunkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, DNSConfigSearches []string) error {

	labels := splunk.GetSplunkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: splunk.GetSplunkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: splunk.GetSplunkHeadlessServiceName(instanceType, identifier),
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: splunk.SPLUNK_IMAGE,
							Name: "splunk",
							Ports: splunk.GetSplunkContainerPorts(),
							Env: envVariables,
						},
					},
					ImagePullSecrets: splunk.GetImagePullSecrets(),
				},
			},
		},
	}

	if DNSConfigSearches != nil {
		statefulSet.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		statefulSet.Spec.Template.Spec.DNSConfig = &corev1.PodDNSConfig{
			Searches: DNSConfigSearches,
		}
	}

	AddOwnerRefToObject(statefulSet, AsOwner(cr))

	err := CreateResource(statefulSet)
	if err != nil {
		return err
	}

	err = CreateService(cr, instanceType, identifier, true)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkStatefulSet(cr *v1alpha1.SplunkInstance, instanceType spark.SparkInstanceType, identifier string, replicas int, envVariables []corev1.EnvVar, DNSConfigSearches []string, containerPorts []corev1.ContainerPort, servicePorts []corev1.ServicePort) error {

	labels := spark.GetSparkAppLabels(identifier, instanceType.ToString())
	replicas32 := int32(replicas)

	numCPU, err := resource.ParseQuantity("1000m")
	if err != nil {
		return err
	}

	numMemory, err := resource.ParseQuantity("2Gi")
	if err != nil {
		return err
	}

	statefulSet := &v1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: spark.GetSparkStatefulsetName(instanceType, identifier),
			Namespace: cr.Namespace,
		},
		Spec: v1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: spark.GetSparkHeadlessServiceName(instanceType, identifier),
			Replicas: &replicas32,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: spark.SPARK_IMAGE,
							Name: "spark",
							Ports: containerPorts,
							Env: envVariables,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: numCPU,
									corev1.ResourceMemory: numMemory,
								},
							},
						},
					},
					ImagePullSecrets: splunk.GetImagePullSecrets(),
				},
			},
		},
	}

	if DNSConfigSearches != nil {
		statefulSet.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		statefulSet.Spec.Template.Spec.DNSConfig = &corev1.PodDNSConfig{
			Searches: DNSConfigSearches,
		}
	}

	AddOwnerRefToObject(statefulSet, AsOwner(cr))

	err = CreateResource(statefulSet)
	if err != nil {
		return err
	}

	err = CreateSparkService(cr, instanceType, identifier, true, servicePorts)
	if err != nil {
		return err
	}

	return nil
}