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
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo"
	ginkgoconfig "github.com/onsi/ginkgo/config"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
)

const (
	defaultOperatorImage = "splunk/splunk-operator"
	defaultSplunkImage   = "splunk/splunk:latest"

	// defaultTestTimeout is the max timeout in seconds before async test failed.
	defaultTestTimeout = 1500

	// PollInterval specifies the polling interval
	PollInterval = 5 * time.Second

	// ConsistentPollInterval is the interval to use to consistently check a state is stable
	ConsistentPollInterval = 200 * time.Millisecond

	// ConsistentDuration is use to check a state is stable
	ConsistentDuration = 2000 * time.Millisecond

	// DefaultTimeout is the max timeout before we failed.
	DefaultTimeout = 5 * time.Minute

	// SearchHeadPod Template String for search head pod
	SearchHeadPod = "splunk-%s-shc-search-head-%d"

	// StandalonePod Template String for standalone pod
	StandalonePod = "splunk-%s-standalone-%d"

	// IndexerPod Template String for indexer pod
	IndexerPod = "splunk-%s-idxc-indexer-%d"

	// IndexerMultisitePod Template String for indexer pod in multisite cluster
	IndexerMultisitePod = "splunk-%s-site%d-indexer-%d"

	// MonitoringConsoleSts Montioring Console Statefulset Template
	MonitoringConsoleSts = "splunk-%s-monitoring-console"

	// MonitoringConsolePod Montioring Console Statefulset Template
	MonitoringConsolePod = "splunk-%s-monitoring-console-%d"

	// ClusterMasterPod ClusterMaster Pod Template String
	ClusterMasterPod = "splunk-%s-cluster-master-0"

	// MultiSiteIndexerPod Indexer Pod Template String
	MultiSiteIndexerPod = "splunk-%s-%s-indexer-%d"

	// SecretObjectName Secret object Template
	SecretObjectName = "splunk-%s-secret"
)

var (
	metricsHost              = "0.0.0.0"
	metricsPort              = 8383
	specifiedOperatorImage   = defaultOperatorImage
	specifiedSplunkImage     = defaultSplunkImage
	specifiedSkipTeardown    = false
	specifiedLicenseFilePath = ""
	specifiedTestTimeout     = defaultTestTimeout
	specifiedCommitHash      = ""
)

type cleanupFunc func() error

// TestEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run tests against
type TestEnv struct {
	kubeAPIServer      string
	name               string
	namespace          string
	serviceAccountName string
	roleName           string
	roleBindingName    string
	operatorName       string
	operatorImage      string
	splunkImage        string
	initialized        bool
	SkipTeardown       bool
	licenseFilePath    string
	licenseCMName      string
	s3IndexSecret      string
	kubeClient         client.Client
	Log                logr.Logger
	cleanupFuncs       []cleanupFunc
}

func init() {
	l := zap.LoggerTo(ginkgo.GinkgoWriter)
	l.WithName("testenv")
	logf.SetLogger(l)

	flag.StringVar(&specifiedLicenseFilePath, "license-file", "", "Enterprise license file to use")
	flag.StringVar(&specifiedOperatorImage, "operator-image", defaultOperatorImage, "Splunk Operator image to use")
	flag.StringVar(&specifiedSplunkImage, "splunk-image", defaultSplunkImage, "Splunk Enterprise (splunkd) image to use")
	flag.BoolVar(&specifiedSkipTeardown, "skip-teardown", false, "True to skip tearing down the test env after use")
	flag.IntVar(&specifiedTestTimeout, "test-timeout", defaultTestTimeout, "Max test timeout in seconds to use")
	flag.StringVar(&specifiedCommitHash, "commit-hash", "", "commit hash string to use as part of the name")
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// NewDefaultTestEnv creates a default test environment
func NewDefaultTestEnv(name string) (*TestEnv, error) {
	return NewTestEnv(name, specifiedCommitHash, specifiedOperatorImage, specifiedSplunkImage, specifiedLicenseFilePath)
}

// NewTestEnv creates a new test environment to run tests againsts
func NewTestEnv(name, commitHash, operatorImage, splunkImage, licenseFilePath string) (*TestEnv, error) {
	var envName string
	if commitHash == "" {
		envName = name
	} else {
		envName = commitHash + "-" + name
	}

	// The name are used in various resource label and there is a 63 char limit. Do our part to make sure we do not exceed that limit
	if len(envName) > 24 {
		return nil, fmt.Errorf("both %s and %s combined have exceeded 24 chars", name, commitHash)
	}

	testenv := &TestEnv{
		name:               envName,
		namespace:          envName,
		serviceAccountName: envName,
		roleName:           envName,
		roleBindingName:    envName,
		operatorName:       "splunk-op-" + envName,
		operatorImage:      operatorImage,
		splunkImage:        splunkImage,
		SkipTeardown:       specifiedSkipTeardown,
		licenseCMName:      envName,
		licenseFilePath:    licenseFilePath,
		s3IndexSecret:      "splunk-s3-index-" + envName,
	}

	testenv.Log = logf.Log.WithValues("testenv", testenv.name)

	// Scheme
	enterprisev1.SchemeBuilder.AddToScheme(scheme.Scheme)

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	testenv.kubeAPIServer = cfg.Host
	testenv.Log.Info("Using kube-apiserver\n", "kube-apiserver", cfg.Host)

	//
	metricsAddr := fmt.Sprintf("%s:%d", metricsHost, metricsPort+ginkgoconfig.GinkgoConfig.ParallelNode)

	kubeManager, err := manager.New(cfg, manager.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: metricsAddr,
	})
	if err != nil {
		return nil, err
	}

	testenv.kubeClient = kubeManager.GetClient()
	if testenv.kubeClient == nil {
		return nil, fmt.Errorf("kubeClient is nil")
	}

	// We need to start the manager to setup the cache. Otherwise, we have to
	// use apireader instead of kubeclient when retrieving resources
	go func() {
		err := kubeManager.Start(signals.SetupSignalHandler())
		if err != nil {
			panic("Unable to start kube manager. Error: " + err.Error())
		}
	}()

	if err := testenv.setup(); err != nil {
		// teardown() should still be invoked
		return nil, err
	}

	return testenv, nil
}

//GetName returns the name of the testenv
func (testenv *TestEnv) GetName() string {
	return testenv.name
}

func (testenv *TestEnv) setup() error {
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

	// Create s3 secret object for index test
	testenv.createIndexSecret()

	if testenv.licenseFilePath != "" {
		err = testenv.createLicenseConfigMap()
		if err != nil {
			return err
		}
	}
	testenv.initialized = true
	testenv.Log.Info("testenv initialized.\n", "namespace", testenv.namespace, "operatorImage", testenv.operatorImage, "splunkImage", testenv.splunkImage)
	return nil
}

// Teardown cleanup the resources use in this testenv
func (testenv *TestEnv) Teardown() error {

	if testenv.SkipTeardown {
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

func (testenv *TestEnv) pushCleanupFunc(fn cleanupFunc) {
	testenv.cleanupFuncs = append(testenv.cleanupFuncs, fn)
}

func (testenv *TestEnv) popCleanupFunc() (cleanupFunc, error) {
	if len(testenv.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := testenv.cleanupFuncs[len(testenv.cleanupFuncs)-1]
	testenv.cleanupFuncs = testenv.cleanupFuncs[:len(testenv.cleanupFuncs)-1]

	return fn, nil
}

func (testenv *TestEnv) createNamespace() error {

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

func (testenv *TestEnv) createSA() error {
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

func (testenv *TestEnv) createRole() error {
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

func (testenv *TestEnv) createRoleBinding() error {
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

func (testenv *TestEnv) createOperator() error {
	//op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage, "nil")
	op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage)
	err := testenv.GetKubeClient().Create(context.TODO(), op)
	if err != nil {
		testenv.Log.Error(err, "Unable to create operator")
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

	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
		deployment := &appsv1.Deployment{}
		err := testenv.GetKubeClient().Get(context.TODO(), key, deployment)
		if err != nil {
			return false, err
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
		testenv.Log.Error(err, "Unable to create operator")
		return err
	}
	return nil
}

// CreateLicenseConfigMap sets the license file path and create config map.
// Required if license file path is not present during TestEnv initialization
func (testenv *TestEnv) CreateLicenseConfigMap(path string) error {
	testenv.licenseFilePath = path
	err := testenv.createLicenseConfigMap()
	return err
}

func (testenv *TestEnv) createLicenseConfigMap() error {
	lic, err := newLicenseConfigMap(testenv.licenseCMName, testenv.namespace, testenv.licenseFilePath)
	if err != nil {
		return err
	}
	if err := testenv.GetKubeClient().Create(context.TODO(), lic); err != nil {
		testenv.Log.Error(err, "Unable to create license configmap")
		return err
	}

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

// Create a service account config
func newServiceAccount(ns string, serviceAccountName string) *corev1.ServiceAccount {
	new := corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: ns,
		},
	}

	return &new
}

// CreateServiceAccount Create a service account with given name
func (testenv *TestEnv) CreateServiceAccount(name string) error {
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
func (testenv *TestEnv) createIndexSecret() error {
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

// GetIndexSecretName return index secret object name
func (testenv *TestEnv) GetIndexSecretName() string {
	return testenv.s3IndexSecret
}

// NewDeployment creates a new deployment
func (testenv *TestEnv) NewDeployment(name string) (*Deployment, error) {
	d := Deployment{
		name:              testenv.GetName() + "-" + name,
		testenv:           testenv,
		testTimeoutInSecs: time.Duration(specifiedTestTimeout) * time.Second,
	}

	return &d, nil
}
