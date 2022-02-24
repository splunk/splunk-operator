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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestCaseEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run test cases against
type TestCaseEnv struct {
	kubeClient         client.Client
	name               string
	namespace          string
	serviceAccountName string
	roleName           string
	roleBindingName    string
	initialized        bool
	SkipTeardown       bool
	licenseFilePath    string
	licenseCMName      string
	s3IndexSecret      string
	Log                logr.Logger
	cleanupFuncs       []cleanupFunc
	debug              string
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestCaseEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// NewDefaultTestCaseEnv creates a default test environment
func NewDefaultTestCaseEnv(kubeClient client.Client, name string) (*TestCaseEnv, error) {
	return NewTestCaseEnv(kubeClient, name, specifiedLicenseFilePath)
}

// NewTestCaseEnv creates a new test environment to run tests againsts
func NewTestCaseEnv(kubeClient client.Client, name, licenseFilePath string) (*TestCaseEnv, error) {
	var envName string

	// The name are used in various resource label and there is a 63 char limit. Do our part to make sure we do not exceed that limit
	if len(envName) > 24 {
		return nil, fmt.Errorf("name %s has exceeded 24 chars", name)
	}

	testcaseenv := &TestCaseEnv{
		kubeClient:         kubeClient,
		name:               name,
		namespace:          name,
		serviceAccountName: name,
		roleName:           name,
		roleBindingName:    name,
		SkipTeardown:       specifiedSkipTeardown,
		licenseCMName:      name,
		licenseFilePath:    licenseFilePath,
		s3IndexSecret:      "splunk-s3-index-" + name,
		debug:              os.Getenv("DEBUG"),
	}

	testcaseenv.Log = logf.Log.WithValues("testcaseenv", testcaseenv.name)

	if err := testcaseenv.setup(); err != nil {
		// teardown() should still be invoked
		return nil, err
	}

	return testcaseenv, nil
}

//GetName returns the name of the testcaseenv
func (testcaseenv *TestCaseEnv) GetName() string {
	return testcaseenv.name
}

func (testcaseenv *TestCaseEnv) setup() error {
	testcaseenv.Log.Info("testcaseenv initializing.\n")

	var err error
	err = testcaseenv.createNamespace()
	if err != nil {
		return err
	}

	err = testcaseenv.createSA()
	if err != nil {
		return err
	}

	// Create s3 secret object for index test
	testcaseenv.createIndexSecret()

	if testcaseenv.licenseFilePath != "" {
		err = testcaseenv.createLicenseConfigMap()
		if err != nil {
			return err
		}
	}
	testcaseenv.initialized = true
	testcaseenv.Log.Info("testcaseenv initialized.\n", "namespace", testcaseenv.namespace)
	return nil
}

// Teardown cleanup the resources use in this testcaseenv
func (testcaseenv *TestCaseEnv) Teardown() error {

	if testcaseenv.SkipTeardown && testcaseenv.debug == "True" {
		testcaseenv.Log.Info("testcaseenv teardown is skipped!\n")
		return nil
	}

	testcaseenv.initialized = false

	for fn, err := testcaseenv.popCleanupFunc(); err == nil; fn, err = testcaseenv.popCleanupFunc() {
		cleanupErr := fn()
		if cleanupErr != nil {
			testcaseenv.Log.Error(cleanupErr, "CleanupFunc returns an error. Attempt to continue.\n")
		}
	}

	testcaseenv.Log.Info("testcaseenv deleted.\n")
	return nil
}

func (testcaseenv *TestCaseEnv) pushCleanupFunc(fn cleanupFunc) {
	testcaseenv.cleanupFuncs = append(testcaseenv.cleanupFuncs, fn)
}

func (testcaseenv *TestCaseEnv) popCleanupFunc() (cleanupFunc, error) {
	if len(testcaseenv.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := testcaseenv.cleanupFuncs[len(testcaseenv.cleanupFuncs)-1]
	testcaseenv.cleanupFuncs = testcaseenv.cleanupFuncs[:len(testcaseenv.cleanupFuncs)-1]

	return fn, nil
}

func (testcaseenv *TestCaseEnv) createNamespace() error {

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testcaseenv.namespace,
		},
	}

	err := testcaseenv.GetKubeClient().Create(context.TODO(), namespace)
	if err != nil {
		return err
	}

	// Cleanup the namespace when we teardown this testcaseenv
	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), namespace)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete namespace")
			return err
		}
		if err = wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
			key := client.ObjectKey{Name: testcaseenv.namespace, Namespace: testcaseenv.namespace}
			ns := &corev1.Namespace{}
			err := testcaseenv.GetKubeClient().Get(context.TODO(), key, ns)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if ns.Status.Phase == corev1.NamespaceTerminating {
				return false, nil
			}

			return true, nil
		}); err != nil {
			testcaseenv.Log.Error(err, "Unable to delete namespace")
			return err
		}

		return nil
	})

	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testcaseenv.namespace}
		ns := &corev1.Namespace{}
		err := testcaseenv.GetKubeClient().Get(context.TODO(), key, ns)
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
		testcaseenv.Log.Error(err, "Unable to get namespace")
		return err
	}

	return nil
}

func (testcaseenv *TestCaseEnv) createSA() error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testcaseenv.serviceAccountName,
			Namespace: testcaseenv.namespace,
		},
	}

	err := testcaseenv.GetKubeClient().Create(context.TODO(), sa)
	if err != nil {
		testcaseenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), sa)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})

	return nil
}

func (testcaseenv *TestCaseEnv) createRole() error {
	role := newRole(testcaseenv.roleName, testcaseenv.namespace)

	err := testcaseenv.GetKubeClient().Create(context.TODO(), role)
	if err != nil {
		testcaseenv.Log.Error(err, "Unable to create role")
		return err
	}

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), role)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete role")
			return err
		}
		return nil
	})

	return nil
}

func (testcaseenv *TestCaseEnv) createRoleBinding() error {
	binding := newRoleBinding(testcaseenv.roleBindingName, testcaseenv.serviceAccountName, testcaseenv.namespace, testcaseenv.roleName)

	err := testcaseenv.GetKubeClient().Create(context.TODO(), binding)
	if err != nil {
		testcaseenv.Log.Error(err, "Unable to create rolebinding")
		return err
	}

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), binding)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete rolebinding")
			return err
		}
		return nil
	})

	return nil
}

// CreateLicenseConfigMap sets the license file path and create config map.
// Required if license file path is not present during TestCaseEnv initialization
func (testcaseenv *TestCaseEnv) CreateLicenseConfigMap(path string) error {
	testcaseenv.licenseFilePath = path
	err := testcaseenv.createLicenseConfigMap()
	return err
}

func (testcaseenv *TestCaseEnv) createLicenseConfigMap() error {
	lic, err := newLicenseConfigMap(testcaseenv.licenseCMName, testcaseenv.namespace, testcaseenv.licenseFilePath)
	if err != nil {
		return err
	}

	// Check if config map already exists
	key := client.ObjectKey{Name: testcaseenv.namespace, Namespace: testcaseenv.namespace}
	err = testcaseenv.GetKubeClient().Get(context.TODO(), key, lic)

	if err != nil {
		testcaseenv.Log.Info("No Existing license config map not found. Creating a new License Configmap", "Name", testcaseenv.namespace)
	} else {
		testcaseenv.Log.Info("Existing license config map found.", "License Config Map Name", testcaseenv.namespace)
		return nil
	}

	// Create a new licese config map
	err = testcaseenv.GetKubeClient().Create(context.TODO(), lic)
	if err != nil {
		testcaseenv.Log.Error(err, "Unable to create license configmap")
		return err
	}

	testcaseenv.Log.Info("New License Config Map created.", "License Config Map Name", testcaseenv.namespace)

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), lic)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete license configmap ")
			return err
		}
		return nil
	})

	return nil
}

// CreateServiceAccount Create a service account with given name
func (testcaseenv *TestCaseEnv) CreateServiceAccount(name string) error {
	serviceAccountConfig := newServiceAccount(testcaseenv.namespace, name)
	if err := testcaseenv.GetKubeClient().Create(context.TODO(), serviceAccountConfig); err != nil {
		testcaseenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), serviceAccountConfig)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})
	return nil
}

// CreateIndexSecret create secret object
func (testcaseenv *TestCaseEnv) createIndexSecret() error {
	secretName := testcaseenv.s3IndexSecret
	ns := testcaseenv.namespace
	data := map[string][]byte{"s3_access_key": []byte(os.Getenv("AWS_ACCESS_KEY_ID")),
		"s3_secret_key": []byte(os.Getenv("AWS_SECRET_ACCESS_KEY"))}
	secret := newSecretSpec(ns, secretName, data)
	if err := testcaseenv.GetKubeClient().Create(context.TODO(), secret); err != nil {
		testcaseenv.Log.Error(err, "Unable to create s3 index secret object")
		return err
	}

	testcaseenv.pushCleanupFunc(func() error {
		err := testcaseenv.GetKubeClient().Delete(context.TODO(), secret)
		if err != nil {
			testcaseenv.Log.Error(err, "Unable to delete s3 index secret object")
			return err
		}
		return nil
	})
	return nil
}

// GetIndexSecretName return index secret object name
func (testcaseenv *TestCaseEnv) GetIndexSecretName() string {
	return testcaseenv.s3IndexSecret
}

// GetLMConfigMap Return name of license config map
func (testcaseenv *TestCaseEnv) GetLMConfigMap() string {
	return testcaseenv.licenseCMName
}

// NewDeployment creates a new deployment
func (testcaseenv *TestCaseEnv) NewDeployment(name string) (*Deployment, error) {
	d := Deployment{
		name:              testcaseenv.GetName() + "-" + name,
		testenv:           testcaseenv,
		testTimeoutInSecs: time.Duration(specifiedTestTimeout) * time.Second,
	}

	return &d, nil
}
