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
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestCaseEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run test cases against
type TestCaseEnv struct {
	kubeClient          client.Client
	name                string
	namespace           string
	serviceAccountName  string
	roleName            string
	roleBindingName     string
	operatorName        string
	operatorImage       string
	splunkImage         string
	initialized         bool
	SkipTeardown        bool
	licenseFilePath     string
	licenseCMName       string
	s3IndexSecret       string
	Log                 logr.Logger
	cleanupFuncs        []cleanupFunc
	debug               string
	clusterWideOperator string
	clusterProvider     string
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestCaseEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// NewDefaultTestCaseEnv creates a default test environment
func NewDefaultTestCaseEnv(kubeClient client.Client, name string) (*TestCaseEnv, error) {
	return NewTestCaseEnv(kubeClient, name, specifiedOperatorImage, specifiedSplunkImage, specifiedLicenseFilePath)
}

// NewTestCaseEnv creates a new test environment to run tests againsts
func NewTestCaseEnv(kubeClient client.Client, name string, operatorImage string, splunkImage string, licenseFilePath string) (*TestCaseEnv, error) {
	var envName string

	// The name are used in various resource label and there is a 63 char limit. Do our part to make sure we do not exceed that limit
	if len(envName) > 24 {
		return nil, fmt.Errorf("name %s has exceeded 24 chars", name)
	}

	testenv := &TestCaseEnv{
		kubeClient:          kubeClient,
		name:                name,
		namespace:           name,
		serviceAccountName:  name,
		roleName:            name,
		roleBindingName:     name,
		operatorName:        "splunk-op-" + name,
		operatorImage:       operatorImage,
		splunkImage:         splunkImage,
		SkipTeardown:        specifiedSkipTeardown,
		licenseCMName:       name,
		licenseFilePath:     licenseFilePath,
		s3IndexSecret:       "splunk-s3-index-" + name,
		debug:               os.Getenv("DEBUG"),
		clusterWideOperator: installOperatorClusterWide,
		clusterProvider:     os.Getenv("CLUSTER_PROVIDER"),
	}

	testenv.Log = logf.Log.WithValues("testcaseenv", testenv.name)

	if err := testenv.setup(); err != nil {
		// teardown() should still be invoked
		return nil, err
	}

	return testenv, nil
}

//GetName returns the name of the testenv
func (testenv *TestCaseEnv) GetName() string {
	return testenv.name
}

//IsOperatorInstalledClusterWide returns if operator is installed clusterwide
func (testenv *TestCaseEnv) IsOperatorInstalledClusterWide() string {
	return testenv.clusterWideOperator
}

func (testenv *TestCaseEnv) setup() error {
	testenv.Log.Info("testenv initializing.\n")

	var err error
	err = testenv.createNamespace()
	if err != nil {
		return err
	}

	err = testenv.createSA()
	if err != nil {
		return err
	}

	if installOperatorClusterWide != "true" {
		err = testenv.createRole()
		if err != nil {
			return err
		}

		err = testenv.createRoleBinding()
		if err != nil {
			return err
		}

		err = testenv.createOperator()
		if err != nil {
			return err
		}
	}

	// Create secret object for index test
	switch ClusterProvider {
	case "eks":
		testenv.createIndexSecret()
	case "azure":
		testenv.createIndexSecretAzure()
	default:
		testenv.Log.Info("Failed to create secret object")
	}

	if testenv.licenseFilePath != "" {
		err = testenv.createLicenseConfigMap()
		if err != nil {
			return err
		}
	}
	testenv.initialized = true
	testenv.Log.Info("testenv initialized.\n", "namespace", testenv.namespace)
	return nil
}

// Teardown cleanup the resources use in this testenv
func (testenv *TestCaseEnv) Teardown() error {

	if testenv.SkipTeardown && testenv.debug == "True" {
		testenv.Log.Info("testenv teardown is skipped!\n")
		return nil
	}

	testenv.initialized = false

	for fn, err := testenv.popCleanupFunc(); err == nil; fn, err = testenv.popCleanupFunc() {
		cleanupErr := fn()
		if cleanupErr != nil {
			testenv.Log.Error(cleanupErr, "CleanupFunc returns an error. Attempt to continue.\n")
		}
	}

	testenv.Log.Info("testenv deleted.\n")
	return nil
}

func (testenv *TestCaseEnv) pushCleanupFunc(fn cleanupFunc) {
	testenv.cleanupFuncs = append(testenv.cleanupFuncs, fn)
}

func (testenv *TestCaseEnv) popCleanupFunc() (cleanupFunc, error) {
	if len(testenv.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := testenv.cleanupFuncs[len(testenv.cleanupFuncs)-1]
	testenv.cleanupFuncs = testenv.cleanupFuncs[:len(testenv.cleanupFuncs)-1]

	return fn, nil
}

func (testenv *TestCaseEnv) createNamespace() error {

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testenv.namespace,
		},
	}

	err := testenv.GetKubeClient().Create(context.TODO(), namespace)
	if err != nil {
		return err
	}

	// Cleanup the namespace when we teardown this testenv
	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), namespace)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete namespace")
			return err
		}
		if err = wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
			key := client.ObjectKey{Name: testenv.namespace, Namespace: testenv.namespace}
			ns := &corev1.Namespace{}
			err := testenv.GetKubeClient().Get(context.TODO(), key, ns)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if ns.Status.Phase == corev1.NamespaceTerminating {
				return false, nil
			}

			return true, nil
		}); err != nil {
			testenv.Log.Error(err, "Unable to delete namespace")
			return err
		}

		return nil
	})

	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testenv.namespace}
		ns := &corev1.Namespace{}
		err := testenv.GetKubeClient().Get(context.TODO(), key, ns)
		if err != nil {
			// Try again
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if ns.Status.Phase == corev1.NamespaceActive {
			return true, nil
		}

		return false, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to get namespace")
		return err
	}

	return nil
}

func (testenv *TestCaseEnv) createSA() error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testenv.serviceAccountName,
			Namespace: testenv.namespace,
		},
	}

	err := testenv.GetKubeClient().Create(context.TODO(), sa)
	if err != nil {
		testenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), sa)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) createRole() error {
	role := newRole(testenv.roleName, testenv.namespace)

	err := testenv.GetKubeClient().Create(context.TODO(), role)
	if err != nil {
		testenv.Log.Error(err, "Unable to create role")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), role)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete role")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) createRoleBinding() error {
	binding := newRoleBinding(testenv.roleBindingName, testenv.serviceAccountName, testenv.namespace, testenv.roleName)

	err := testenv.GetKubeClient().Create(context.TODO(), binding)
	if err != nil {
		testenv.Log.Error(err, "Unable to create rolebinding")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), binding)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete rolebinding")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) attachPVCToOperator(name string) error {
	var err error

	// volume name which refers to PVC to be attached
	volumeName := "app-staging"

	namespacedName := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
	operator := &appsv1.Deployment{}
	err = testenv.GetKubeClient().Get(context.TODO(), namespacedName, operator)
	if err != nil {
		testenv.Log.Error(err, "Unable to get operator", "operator name", testenv.operatorName)
		return err
	}

	volume := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: name,
			},
		},
	}

	operator.Spec.Template.Spec.Volumes = append(operator.Spec.Template.Spec.Volumes, volume)

	volumeMount := corev1.VolumeMount{
		Name:      volumeName,
		MountPath: splcommon.AppDownloadVolume,
	}

	operator.Spec.Template.Spec.Containers[0].VolumeMounts = append(operator.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMount)

	// update the operator deployment now
	err = testenv.GetKubeClient().Update(context.TODO(), operator)
	if err != nil {
		testenv.Log.Error(err, "Unable to update operator", "operator name", testenv.operatorName)
		return err
	}

	return err
}

func (testenv *TestCaseEnv) createOperator() error {
	//op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage, "nil")
	op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage)
	err := testenv.GetKubeClient().Create(context.TODO(), op)
	if err != nil {
		testenv.Log.Error(err, "Unable to create operator")
		return err
	}

	// create the PVC to attach to operator for downloading apps
	pvc, err := newPVC(appDownlodPVCName, testenv.namespace, DefaultStorageForAppDownloads, DefaultStorageClassName)
	if err != nil {
		testenv.Log.Error(err, "Unable to create PVC", "pvcName", pvc.ObjectMeta.Name)
		return err
	}
	err = testenv.GetKubeClient().Create(context.TODO(), pvc)
	if err != nil {
		testenv.Log.Error(err, "Unable to create PVC")
		return err
	}

	//attach the PVC to operator
	err = testenv.attachPVCToOperator(pvc.ObjectMeta.Name)
	if err != nil {
		testenv.Log.Error(err, "Unable to attach PVC to operator", "pvcName", pvc.ObjectMeta.Name)
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), op)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete operator")
			return err
		}
		return nil
	})

	OperatorInstallationTimeout := 5 * time.Minute
	if err := wait.PollImmediate(PollInterval, OperatorInstallationTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
		deployment := &appsv1.Deployment{}
		err := testenv.GetKubeClient().Get(context.TODO(), key, deployment)
		if err != nil {
			testenv.Log.Error(err, "operator not found waiting")
			return false, nil
		}

		DumpGetPods(testenv.namespace)
		if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
			return false, nil
		}

		if deployment.Status.ReadyReplicas < *op.Spec.Replicas {
			return false, nil
		}

		return true, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to find operator after creation")
		return err
	}
	return nil
}

// CreateLicenseConfigMap sets the license file path and create config map.
// Required if license file path is not present during TestCaseEnv initialization
func (testenv *TestCaseEnv) CreateLicenseConfigMap(path string) error {
	testenv.licenseFilePath = path
	err := testenv.createLicenseConfigMap()
	return err
}

func (testenv *TestCaseEnv) createLicenseConfigMap() error {
	lic, err := newLicenseConfigMap(testenv.licenseCMName, testenv.namespace, testenv.licenseFilePath)
	if err != nil {
		return err
	}

	// Check if config map already exists
	key := client.ObjectKey{Name: testenv.namespace, Namespace: testenv.namespace}
	err = testenv.GetKubeClient().Get(context.TODO(), key, lic)

	if err != nil {
		testenv.Log.Info("No Existing license config map not found. Creating a new License Configmap", "Name", testenv.namespace)
	} else {
		testenv.Log.Info("Existing license config map found.", "License Config Map Name", testenv.namespace)
		return nil
	}

	// Create a new licese config map
	err = testenv.GetKubeClient().Create(context.TODO(), lic)
	if err != nil {
		testenv.Log.Error(err, "Unable to create license configmap")
		return err
	}

	testenv.Log.Info("New License Config Map created.", "License Config Map Name", testenv.namespace)

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), lic)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete license configmap ")
			return err
		}
		return nil
	})

	return nil
}

// CreateServiceAccount Create a service account with given name
func (testenv *TestCaseEnv) CreateServiceAccount(name string) error {
	serviceAccountConfig := newServiceAccount(testenv.namespace, name)
	if err := testenv.GetKubeClient().Create(context.TODO(), serviceAccountConfig); err != nil {
		testenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), serviceAccountConfig)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})
	return nil
}

// CreateIndexSecret create secret object
func (testenv *TestCaseEnv) createIndexSecret() error {
	secretName := testenv.s3IndexSecret
	ns := testenv.namespace
	data := map[string][]byte{"s3_access_key": []byte(os.Getenv("AWS_ACCESS_KEY_ID")),
		"s3_secret_key": []byte(os.Getenv("AWS_SECRET_ACCESS_KEY"))}
	secret := newSecretSpec(ns, secretName, data)
	if err := testenv.GetKubeClient().Create(context.TODO(), secret); err != nil {
		testenv.Log.Error(err, "Unable to create s3 index secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete s3 index secret object")
			return err
		}
		return nil
	})
	return nil
}

// createIndexSecretAzure create secret object for Azure
func (testenv *TestCaseEnv) createIndexSecretAzure() error {
	secretName := testenv.s3IndexSecret
	ns := testenv.namespace
	data := map[string][]byte{"azure_sa_name": []byte(os.Getenv("STORAGE_ACCOUNT")),
		"azure_sa_secret_key": []byte(os.Getenv("STORAGE_ACCOUNT_KEY"))}
	secret := newSecretSpec(ns, secretName, data)
	if err := testenv.GetKubeClient().Create(context.TODO(), secret); err != nil {
		testenv.Log.Error(err, "Unable to create Azure index secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete Azure index secret object")
			return err
		}
		return nil
	})
	return nil
}

// GetIndexSecretName return index secret object name
func (testenv *TestCaseEnv) GetIndexSecretName() string {
	return testenv.s3IndexSecret
}

// GetLMConfigMap Return name of license config map
func (testenv *TestCaseEnv) GetLMConfigMap() string {
	return testenv.licenseCMName
}

// NewDeployment creates a new deployment
func (testenv *TestCaseEnv) NewDeployment(name string) (*Deployment, error) {
	d := Deployment{
		name:              testenv.GetName() + "-" + name,
		testenv:           testenv,
		testTimeoutInSecs: time.Duration(SpecifiedTestTimeout) * time.Second,
	}

	return &d, nil
}
