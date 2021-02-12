// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package testenv

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

const (
	letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func init() {
	rand.Seed(time.Now().UnixNano())
	l := zap.LoggerTo(ginkgo.GinkgoWriter)
	l.WithName("util")
	logf.SetLogger(l)
}

// RandomDNSName returns a random string that is a valid DNS name
func RandomDNSName(n int) string {
	b := make([]byte, n)
	for i := range b {
		// Must start with letter
		if i == 0 {
			b[i] = letterBytes[rand.Intn(25)]
		} else {
			b[i] = letterBytes[rand.Intn(len(letterBytes))]
		}
	}
	return string(b)
}

// newStandalone creates and initializes CR for Standalone Kind
func newStandalone(name, ns string) *enterprisev1.Standalone {

	new := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.StandaloneSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	return &new
}

// newStandalone creates and initializes CR for Standalone Kind
func newStandaloneWithGivenSpec(name, ns string, spec enterprisev1.StandaloneSpec) *enterprisev1.Standalone {

	new := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: spec,
	}
	return &new
}

func newLicenseMaster(name, ns, licenseConfigMapName string) *enterprisev1.LicenseMaster {
	new := enterprisev1.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.LicenseMasterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "licenses",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: licenseConfigMapName,
								},
							},
						},
					},
				},
				// TODO: Ensure the license file is actually called "enterprise.lic" when creating the config map
				LicenseURL: "/mnt/licenses/enterprise.lic",
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
			},
		},
	}

	return &new
}

// newClusterMaster creates and initialize the CR for ClusterMaster Kind
func newClusterMaster(name, ns, licenseMasterName string, ansibleConfig string) *enterprisev1.ClusterMaster {
	new := enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.ClusterMasterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterName,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

// newIndexerCluster creates and initialize the CR for IndexerCluster Kind
func newIndexerCluster(name, ns, licenseMasterName string, replicas int, clusterMasterRef string, ansibleConfig string) *enterprisev1.IndexerCluster {
	new := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.IndexerClusterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				Defaults: ansibleConfig,
			},
			Replicas: int32(replicas),
		},
	}

	return &new
}

func newSearchHeadCluster(name, ns, clusterMasterRef, licenseMasterName string, ansibleConfig string) *enterprisev1.SearchHeadCluster {
	new := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.SearchHeadClusterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterName,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

func newRole(name, ns string) *rbacv1.Role {
	new := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints", "persistentvolumeclaims", "configmaps", "secrets", "pods", "serviceaccounts", "pods/exec"},
				Verbs:     []string{"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "damonsets", "replicasets", "statefulsets"},
				Verbs:     []string{"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"enterprise.splunk.com"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}

	return &new
}

func newRoleBinding(name, subject, ns, role string) *rbacv1.RoleBinding {
	binding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      subject,
				Namespace: ns,
			},
		},

		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     role,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	return &binding
}

func newLicenseConfigMap(name, ns, localLicenseFilePath string) (*corev1.ConfigMap, error) {

	data, err := ioutil.ReadFile(localLicenseFilePath)
	if err != nil {
		return nil, err
	}

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: map[string]string{
			path.Base(localLicenseFilePath): string(data),
		},
	}

	return &cm, nil
}

func newOperator(name, ns, account, operatorImageAndTag, splunkEnterpriseImageAndTag, sparkImageAndTag string) *appsv1.Deployment {
	var replicas int32 = 1

	operator := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},

		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "splunk-operator",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": "splunk-operator",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: account,
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           operatorImageAndTag,
							ImagePullPolicy: "IfNotPresent",
							Env: []corev1.EnvVar{
								{
									Name: "WATCH_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								}, {
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								}, {
									Name:  "OPERATOR_NAME",
									Value: "splunk-operator",
								}, {
									Name:  "RELATED_IMAGE_SPLUNK_ENTERPRISE",
									Value: splunkEnterpriseImageAndTag,
								}, {
									Name:  "RELATED_IMAGE_SPLUNK_SPARK",
									Value: sparkImageAndTag,
								},
							},
						},
					},
				},
			},
		},
	}

	return &operator
}

// newStandaloneWithLM creates and initializes CR for Standalone Kind with License Master
func newStandaloneWithLM(name, ns string, licenseMasterName string) *enterprisev1.Standalone {

	new := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterprisev1.StandaloneSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterName,
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	return &new
}

// newSecretSpec create spec for smartstore secret object
func newSecretSpec(ns string, secretName string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "apps/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
		},
		Data: data,
		Type: "Opaque",
	}
	return secret
}

// newStandaloneWithSpec creates and initializes CR for Standalone Kind with given spec
func newStandaloneWithSpec(name, ns string, spec enterprisev1.StandaloneSpec) *enterprisev1.Standalone {

	new := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: spec,
	}
	return &new
}

// DumpGetPods prints list of pods in the namespace
func DumpGetPods(ns string) {
	output, err := exec.Command("kubectl", "get", "pods", "-n", ns).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
	} else {
		for _, line := range strings.Split(string(output), "\n") {
			logf.Log.Info(line)
		}
	}
}
