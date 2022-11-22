// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/onsi/ginkgo"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func init() {
	rand.Seed(time.Now().UnixNano())
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseFlagOptions(&opts)).WithName("util"))

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
func newStandalone(name, ns string) *enterpriseApi.Standalone {

	new := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	return &new
}

// newStandalone creates and initializes CR for Standalone Kind
func newStandaloneWithGivenSpec(name, ns string, spec enterpriseApi.StandaloneSpec) *enterpriseApi.Standalone {

	new := enterpriseApi.Standalone{
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

func newLicenseManager(name, ns, licenseConfigMapName string) *enterpriseApi.LicenseManager {
	new := enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
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
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
			},
		},
	}

	return &new
}

func newLicenseMaster(name, ns, licenseConfigMapName string) *enterpriseApiV3.LicenseMaster {
	new := enterpriseApiV3.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApiV3.LicenseMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
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
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
			},
		},
	}

	return &new
}

// swapClusterManager Enables both CRDs to be tested with minimal duplication
func swapClusterManager(name string, clusterManagerName string) (string, string) {
	clusterMasterName := ""
	if strings.Contains(name, "master") {
		clusterMasterName, clusterManagerName = clusterManagerName, clusterMasterName
	}
	return clusterMasterName, clusterManagerName
}

// swapLicenseManager Enables both License CRDs to be tested with minimal duplication
func swapLicenseManager(name string, licenseManagerName string) (string, string) {
	licenseMasterName := ""
	if strings.Contains(name, "master") {
		licenseMasterName, licenseManagerName = licenseManagerName, licenseMasterName
	}
	return licenseMasterName, licenseManagerName
}

// newClusterManager creates and initialize the CR for ClusterManager Kind
func newClusterManager(name, ns, licenseManagerName string, ansibleConfig string) *enterpriseApi.ClusterManager {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)

	new := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

// newClusterManager creates and initialize the CR for ClusterManager Kind
func newClusterMaster(name, ns, licenseManagerName string, ansibleConfig string) *enterpriseApiV3.ClusterMaster {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)

	new := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

// newClusterManager creates and initialize the CR for ClusterManager Kind
func newClusterManagerWithGivenIndexes(name, ns, licenseManagerName string, ansibleConfig string, smartstorespec enterpriseApi.SmartStoreSpec) *enterpriseApi.ClusterManager {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)

	new := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.ClusterManagerSpec{
			SmartStore: smartstorespec,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

// newClusterManager creates and initialize the CR for ClusterManager Kind
func newClusterMasterWithGivenIndexes(name, ns, licenseManagerName string, ansibleConfig string, smartstorespec enterpriseApi.SmartStoreSpec) *enterpriseApiV3.ClusterMaster {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)

	new := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApiV3.ClusterMasterSpec{
			SmartStore: smartstorespec,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Defaults: ansibleConfig,
			},
		},
	}

	return &new
}

// newIndexerCluster creates and initialize the CR for IndexerCluster Kind
func newIndexerCluster(name, ns, licenseManagerName string, replicas int, clusterManagerRef string, ansibleConfig string) *enterpriseApi.IndexerCluster {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)
	clusterMasterRef, clusterManagerRef := swapClusterManager(name, clusterManagerRef)

	new := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				ClusterManagerRef: corev1.ObjectReference{
					Name: clusterManagerRef,
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Defaults: ansibleConfig,
			},
			Replicas: int32(replicas),
		},
	}

	return &new
}

func newSearchHeadCluster(name, ns, clusterManagerRef, licenseManagerName string, ansibleConfig string) *enterpriseApi.SearchHeadCluster {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)
	clusterMasterRef, clusterManagerRef := swapClusterManager(name, clusterManagerRef)

	new := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{},
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				ClusterManagerRef: corev1.ObjectReference{
					Name: clusterManagerRef,
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
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
				Verbs:     []string{"create", "delete", "get", "list", "watch"},
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

	data, err := os.ReadFile(localLicenseFilePath)
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

func newPVC(name, ns, storage, storageClassName string) (*corev1.PersistentVolumeClaim, error) {

	storageCapacity, err := splcommon.ParseResourceQuantity(storage, DefaultStorageForAppDownloads)
	if err != nil {
		return nil, err
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageCapacity,
				},
			},
			StorageClassName: &storageClassName,
		},
	}
	return &pvc, nil
}

func newOperator(name, ns, account, operatorImageAndTag, splunkEnterpriseImageAndTag string) *appsv1.Deployment {
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
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &OperatorFSGroup,
					},
					ServiceAccountName: account,
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           operatorImageAndTag,
							ImagePullPolicy: "Always",
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

// newStandaloneWithLM creates and initializes CR for Standalone Kind with License Manager
func newStandaloneWithLM(name, ns string, licenseManagerName string) *enterpriseApi.Standalone {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, licenseManagerName)

	new := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
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
func newStandaloneWithSpec(name, ns string, spec enterpriseApi.StandaloneSpec) *enterpriseApi.Standalone {

	new := enterpriseApi.Standalone{
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

// newMonitoringConsoleSpec returns MC Spec with given name, namespace and license manager Ref
func newMonitoringConsoleSpec(name string, ns string, LicenseManagerRef string) *enterpriseApi.MonitoringConsole {

	licenseMasterRef, licenseManagerRef := swapLicenseManager(name, LicenseManagerRef)

	mcSpec := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Volumes: []corev1.Volume{},
			},
		},
	}
	return &mcSpec
}

// newMonitoringConsoleSpecWithGivenSpec returns MC Spec with given name, namespace and Spec
func newMonitoringConsoleSpecWithGivenSpec(name string, ns string, spec enterpriseApi.MonitoringConsoleSpec) *enterpriseApi.MonitoringConsole {
	mcSpec := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: spec,
	}
	return &mcSpec
}

// DumpGetPods prints and returns list of pods in the namespace
func DumpGetPods(ns string) []string {
	output, err := exec.Command("kubectl", "get", "pods", "-n", ns).Output()
	var splunkPods []string
	if err != nil {
		//cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		//logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		logf.Log.Info(line)
		if strings.HasPrefix(line, "splunk") && !strings.HasPrefix(line, "splunk-op") {
			splunkPods = append(splunkPods, strings.Fields(line)[0])
		}
	}
	return splunkPods
}

// DumpGetTopNodes prints and returns Node load information
func DumpGetTopNodes() []string {
	output, err := exec.Command("kubectl", "top", "nodes").Output()
	var splunkNodes []string
	if err != nil {
		//cmd := "kubectl top nodes"
		//logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	if len(output) > 0 {
		for _, line := range strings.Split(string(output), "\n") {
			if len(line) > 0 {
				logf.Log.Info(line)
				splunkNodes = append(splunkNodes, strings.Fields(line)[0])
			}
		}
	}
	return splunkNodes
}

// DumpGetTopPods prints and returns Node load information
func DumpGetTopPods(ns string) []string {
	output, err := exec.Command("kubectl", "top", "pods", "-n", ns).Output()
	var splunkPods []string
	if err != nil {
		//cmd := fmt.Sprintf("kubectl top pods -n %s", ns)
		//logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	if len(output) > 0 {
		for _, line := range strings.Split(string(output), "\n") {
			if len(line) > 0 {
				logf.Log.Info(line)
				splunkPods = append(splunkPods, strings.Fields(line)[0])
			}
		}
	}
	return splunkPods
}

// GetOperatorPodName returns name of operator pod in the namespace
func GetOperatorPodName(testcaseEnvInst *TestCaseEnv) string {
	var ns string
	if testcaseEnvInst.clusterWideOperator != "true" {
		ns = testcaseEnvInst.GetName()
	} else {
		ns = "splunk-operator"
	}
	output, err := exec.Command("kubectl", "get", "pods", "-n", ns).Output()
	var splunkPods string
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return splunkPods
	}
	for _, line := range strings.Split(string(output), "\n") {
		logf.Log.Info(line)
		if strings.HasPrefix(line, "splunk-operator-controller-manager") {
			splunkPods = strings.Fields(line)[0]
			return splunkPods
		} else if strings.HasPrefix(line, "splunk-op") {
			splunkPods = strings.Fields(line)[0]
			return splunkPods
		}
	}
	return splunkPods
}

// DumpGetPvcs prints and returns list of pvcs in the namespace
func DumpGetPvcs(ns string) []string {
	output, err := exec.Command("kubectl", "get", "pvc", "-n", ns).Output()
	var splunkPvcs []string
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pvc -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		logf.Log.Info(line)
		if strings.HasPrefix(line, "pvc-") {
			splunkPvcs = append(splunkPvcs, strings.Fields(line)[0])
		}
	}
	return splunkPvcs
}

// GetConfLineFromPod gets given config from file on POD
func GetConfLineFromPod(podName string, filePath string, ns string, configName string, stanza string, checkStanza bool) (string, error) {
	var config string
	var err error
	output, err := exec.Command("kubectl", "exec", "-n", ns, podName, "--", "cat", filePath).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl exec -n %s %s -- cat %s", ns, podName, filePath)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return config, err
	}

	var stanzaString string
	stanzaFound := true
	if checkStanza {
		stanzaFound = false
		stanzaString = fmt.Sprintf("[%s]", stanza)
	}
	for _, line := range strings.Split(string(output), "\n") {
		// Check for empty lines to prevent an error in logic below
		if len(line) == 0 {
			continue
		}
		// Look for given config name in file
		if !stanzaFound {
			if strings.HasPrefix(line, stanzaString) {
				stanzaFound = true
			}
			continue
		} else if strings.HasPrefix(line, configName) {
			logf.Log.Info(fmt.Sprintf("Configuration %s found at line %s", configName, line))
			config = line
			break
		}
	}
	if config == "" {
		err = fmt.Errorf("failed to find config %s under stanza %s", configName, stanza)
	}
	return config, err
}

// ExecuteCommandOnPod execute command on given pod and return result
func ExecuteCommandOnPod(ctx context.Context, deployment *Deployment, podName string, stdin string) (string, error) {
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return "", err
	}
	logf.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return stdout, nil
}

// ExecuteCommandOnOperatorPod execute command on given pod and return result
func ExecuteCommandOnOperatorPod(ctx context.Context, deployment *Deployment, podName string, stdin string) (string, error) {
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.OperatorPodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return "", err
	}
	logf.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return stdout, nil
}

// GetConfigMap Gets the config map for a given k8 config map name
func GetConfigMap(ctx context.Context, deployment *Deployment, ns string, configMapName string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := deployment.GetInstance(ctx, configMapName, configMap)
	if err != nil {
		deployment.testenv.Log.Error(err, "Unable to get config map", "Config Map Name", configMap, "Namespace", ns)
	}
	return configMap, err
}

// newClusterManagerWithGivenSpec creates and initialize the CR for ClusterManager Kind
func newClusterManagerWithGivenSpec(name string, ns string, spec enterpriseApi.ClusterManagerSpec) *enterpriseApi.ClusterManager {
	new := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
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

// newClusterManagerWithGivenSpec creates and initialize the CR for ClusterManager Kind
func newClusterMasterWithGivenSpec(name string, ns string, spec enterpriseApiV3.ClusterMasterSpec) *enterpriseApiV3.ClusterMaster {
	new := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
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

// newSearchHeadClusterWithGivenSpec create and initializes CR for Search Cluster Kind with Given Spec
func newSearchHeadClusterWithGivenSpec(name string, ns string, spec enterpriseApi.SearchHeadClusterSpec) *enterpriseApi.SearchHeadCluster {
	new := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
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

// newLicenseManagerWithGivenSpec create and initializes CR for License Manager Kind with Given Spec
func newLicenseManagerWithGivenSpec(name, ns string, spec enterpriseApi.LicenseManagerSpec) *enterpriseApi.LicenseManager {
	new := enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
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

// newLicenseMasterWithGivenSpec create and initializes CR for License Manager Kind with Given Spec
func newLicenseMasterWithGivenSpec(name, ns string, spec enterpriseApiV3.LicenseMasterSpec) *enterpriseApiV3.LicenseMaster {
	new := enterpriseApiV3.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
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

// GetOperatorDirsOrFilesInPath returns subdirectory under given path on the given POD
func GetOperatorDirsOrFilesInPath(ctx context.Context, deployment *Deployment, podName string, path string, dirOnly bool) ([]string, error) {
	var cmd string
	if dirOnly {
		cmd = fmt.Sprintf("cd %s; ls -d */", path)
	} else {
		cmd = fmt.Sprintf("cd %s; ls ", path)
	}
	stdout, err := ExecuteCommandOnOperatorPod(ctx, deployment, podName, cmd)
	if err != nil {
		return nil, err
	}
	dirList := strings.Fields(stdout)
	// Directory are returned with trailing /. The below loop removes the trailing /
	for i, dirName := range dirList {
		dirList[i] = strings.TrimSuffix(dirName, "/")
	}
	return strings.Fields(stdout), err
}

// GetDirsOrFilesInPath returns subdirectory under given path on the given POD
func GetDirsOrFilesInPath(ctx context.Context, deployment *Deployment, podName string, path string, dirOnly bool) ([]string, error) {
	var cmd string
	if dirOnly {
		cmd = fmt.Sprintf("cd %s; ls -d */", path)
	} else {
		cmd = fmt.Sprintf("cd %s; ls ", path)
	}
	stdout, err := ExecuteCommandOnPod(ctx, deployment, podName, cmd)
	if err != nil {
		return nil, err
	}
	dirList := strings.Fields(stdout)
	// Directory are returned with trailing /. The below loop removes the trailing /
	for i, dirName := range dirList {
		dirList[i] = strings.TrimSuffix(dirName, "/")
	}
	return strings.Fields(stdout), err
}

// CompareStringSlices checks if two string slices are matching
func CompareStringSlices(stringOne []string, stringTwo []string) bool {
	if len(stringOne) != len(stringTwo) {
		return false
	}
	sort.Strings(stringOne)
	sort.Strings(stringTwo)
	return reflect.DeepEqual(stringOne, stringTwo)
}

// CheckStringInSlice check if string is present in a slice
func CheckStringInSlice(stringSlice []string, compString string) bool {
	logf.Log.Info("Checking for string in slice", "String", compString, "String Slice", stringSlice)
	for _, item := range stringSlice {
		if strings.Contains(item, compString) {
			return true
		}
	}
	return false
}

// GeneratePodNameSlice returns slice of PodNames based on given key and count.
func GeneratePodNameSlice(formatString string, key string, count int, multisite bool, siteCount int) []string {
	var podNames []string
	if multisite {
		for site := 1; site <= siteCount; site++ {
			for i := 0; i < count; i++ {
				podNames = append(podNames, fmt.Sprintf(formatString, key, site, i))
			}
		}
	} else {
		for i := 0; i < count; i++ {
			podNames = append(podNames, fmt.Sprintf(formatString, key, i))
		}
	}
	return podNames
}

// GetPodsStartTime prints and returns list of pods in namespace and their respective start time
func GetPodsStartTime(ns string) map[string]time.Time {
	splunkPodsStartTime := make(map[string]time.Time)
	splunkPods := DumpGetPods(ns)

	for _, podName := range splunkPods {
		output, _ := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		restResponse := PodDetailsStruct{}
		err := json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			logf.Log.Error(err, "Failed to parse splunk pods")
		}
		podStartTime, _ := time.Parse("2006-01-02T15:04:05Z", restResponse.Status.StartTime)
		splunkPodsStartTime[podName] = podStartTime
	}
	return splunkPodsStartTime
}

// DeletePod Delete pod in the namespace
func DeletePod(ns string, podName string) error {
	_, err := exec.Command("kubectl", "delete", "pod", "-n", ns, podName).Output()
	if err != nil {
		logf.Log.Error(err, "Failed to delete operator pod ", "PodName", podName, "Namespace", ns)
		return err
	}
	return nil
}

// DeleteOperatorPod Delete Operator Pod in the namespace
func DeleteOperatorPod(testcaseEnvInst *TestCaseEnv) error {
	var podName string
	var ns string
	if testcaseEnvInst.clusterWideOperator != "true" {
		ns = testcaseEnvInst.GetName()
	} else {
		ns = "splunk-operator"
	}
	podName = GetOperatorPodName(testcaseEnvInst)

	_, err := exec.Command("kubectl", "delete", "pod", "-n", ns, podName).Output()
	if err != nil {
		logf.Log.Error(err, "Failed to delete operator pod ", "PodName", podName, "Namespace", ns)
		return err
	}
	return nil
}

// DeleteFilesOnOperatorPod Delete files on Operator Pod
func DeleteFilesOnOperatorPod(ctx context.Context, deployment *Deployment, podName string, filenames []string) error {
	for _, filepath := range filenames {
		cmd := fmt.Sprintf("rm -f %s", filepath)
		_, err := ExecuteCommandOnOperatorPod(ctx, deployment, podName, cmd)
		if err != nil {
			logf.Log.Error(err, "Failed to delete file on pod ", "PodName", podName, "location", filepath, "command", cmd)
			return err
		}
	}
	return nil
}

// DumpGetSplunkVersion prints the splunk version installed on pods
func DumpGetSplunkVersion(ctx context.Context, ns string, deployment *Deployment, filterString string) {
	splunkPods := DumpGetPods(ns)
	cmd := "/opt/splunk/bin/splunk -version"
	for _, podName := range splunkPods {
		if strings.Contains(podName, filterString) {
			stdout, err := ExecuteCommandOnPod(ctx, deployment, podName, cmd)
			if err != nil {
				logf.Log.Error(err, "Failed to get splunkd version on the pod", "Pod Name", podName)
			}
			logf.Log.Info("Splunk Version Found", "Pod Name", podName, "Version", string(stdout))
		}
	}
}

// CreateDummyFileOnOperator creates a dummy file of specified size at path provided
func CreateDummyFileOnOperator(ctx context.Context, deployment *Deployment, podName string, filepath string, size string, filename string) error {
	cmd := fmt.Sprintf("cd %s && dd if=/dev/zero of=./%s bs=4k iflag=fullblock,count_bytes count=%s", filepath, filename, size)
	_, err := ExecuteCommandOnOperatorPod(ctx, deployment, podName, cmd)
	if err != nil {
		logf.Log.Error(err, "Failed to create file on the pod", "Pod Name", podName)
		return err
	}
	return nil
}

// DeleteConfigMap Delete configMap in the namespace
func DeleteConfigMap(ns string, ConfigMapName string) error {
	logf.Log.Info("Delete configMap", "configMap Name", ConfigMapName)
	_, err := exec.Command("kubectl", "delete", "configmap", "-n", ns, ConfigMapName).Output()
	if err != nil {
		logf.Log.Error(err, "Failed to delete config Map", "ConfigMap Name", ConfigMapName, "Namespace", ns)
		return err
	}
	return nil
}
