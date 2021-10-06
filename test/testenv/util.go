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
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
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

func newLicenseMaster(name, ns, licenseConfigMapName string) *enterpriseApi.LicenseMaster {
	new := enterpriseApi.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.LicenseMasterSpec{
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
				Spec: splcommon.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
			},
		},
	}

	return &new
}

// newClusterMaster creates and initialize the CR for ClusterMaster Kind
func newClusterMaster(name, ns, licenseMasterName string, ansibleConfig string) *enterpriseApi.ClusterMaster {
	new := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
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

// newClusterMaster creates and initialize the CR for ClusterMaster Kind
func newClusterMasterWithGivenIndexes(name, ns, licenseMasterName string, ansibleConfig string, smartstorespec enterpriseApi.SmartStoreSpec) *enterpriseApi.ClusterMaster {
	new := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},

		Spec: enterpriseApi.ClusterMasterSpec{
			SmartStore: smartstorespec,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
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
func newIndexerCluster(name, ns, licenseMasterName string, replicas int, clusterMasterRef string, ansibleConfig string) *enterpriseApi.IndexerCluster {
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

func newSearchHeadCluster(name, ns, clusterMasterRef, licenseMasterName string, ansibleConfig string) *enterpriseApi.SearchHeadCluster {
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
func newStandaloneWithLM(name, ns string, licenseMasterName string) *enterpriseApi.Standalone {

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

// DumpGetPods prints and returns list of pods in the namespace
func DumpGetPods(ns string) []string {
	output, err := exec.Command("kubectl", "get", "pods", "-n", ns).Output()
	var splunkPods []string
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
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
			logf.Log.Info("Configuration found.", "Config", configName, "Line", line)
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
func ExecuteCommandOnPod(deployment *Deployment, podName string, stdin string) (string, error) {
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return "", err
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	return stdout, nil
}

// GetConfigMap Gets the config map for a given k8 config map name
func GetConfigMap(deployment *Deployment, ns string, configMapName string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := deployment.GetInstance(configMapName, configMap)
	if err != nil {
		deployment.testenv.Log.Error(err, "Unable to get config map", "Config Map Name", configMap, "Namespace", ns)
	}
	return configMap, err
}

// newClusterMasterWithGivenSpec creates and initialize the CR for ClusterMaster Kind
func newClusterMasterWithGivenSpec(name string, ns string, spec enterpriseApi.ClusterMasterSpec) *enterpriseApi.ClusterMaster {
	new := enterpriseApi.ClusterMaster{
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

// newLicenseMasterWithGivenSpec create and initializes CR for License Manager Kind with Given Spec
func newLicenseMasterWithGivenSpec(name, ns string, spec enterpriseApi.LicenseMasterSpec) *enterpriseApi.LicenseMaster {
	new := enterpriseApi.LicenseMaster{
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

// GetDirsOrFilesInPath returns subdirectory under given path on the given POD
func GetDirsOrFilesInPath(deployment *Deployment, podName string, path string, dirOnly bool) ([]string, error) {
	var cmd string
	if dirOnly {
		cmd = fmt.Sprintf("cd %s; ls -d */", path)
	} else {
		cmd = fmt.Sprintf("cd %s; ls ", path)
	}
	stdout, err := ExecuteCommandOnPod(deployment, podName, cmd)
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
	for _, item := range stringSlice {
		if item == compString {
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
