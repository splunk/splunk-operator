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
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo"
	ginkgoconfig "github.com/onsi/ginkgo/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
)

const (
	defaultOperatorInstallation = "false"

	defaultOperatorImage = "splunk/splunk-operator"
	defaultSplunkImage   = "splunk/splunk:latest"

	// defaultTestTimeout is the max timeout in seconds before async test failed.
	defaultTestTimeout = 3000

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

	// DeployerPod Template String for deployer pod
	DeployerPod = "splunk-%s-shc-deployer-0"

	// StandalonePod Template String for standalone pod
	StandalonePod = "splunk-%s-standalone-%d"

	// LicenseManagerPod Template String for standalone pod
	LicenseManagerPod = "splunk-%s-" + splcommon.LicenseManager + "-%d"

	// IndexerPod Template String for indexer pod
	IndexerPod = "splunk-%s-idxc-indexer-%d"

	// PVCString Template String for PVC
	PVCString = "pvc-%s-splunk-%s-%s-%d"

	// MonitoringConsoleSts Monitoring Console Statefulset Template
	MonitoringConsoleSts = "splunk-%s-monitoring-console"

	// MonitoringConsolePod Monitoring Console Pod Template String
	MonitoringConsolePod = "splunk-%s-monitoring-console-0"

	// ClusterManagerPod ClusterMaster Pod Template String
	ClusterManagerPod = "splunk-%s-" + splcommon.ClusterManager + "-0"

	// MultiSiteIndexerPod Indexer Pod Template String
	MultiSiteIndexerPod = "splunk-%s-site%d-indexer-%d"

	// NamespaceScopedSecretObjectName Name Space Scoped Secret object Template
	NamespaceScopedSecretObjectName = "splunk-%s-secret"

	// VersionedSecretName Versioned Secret object Template
	VersionedSecretName = "splunk-%s-%s-secret-v%d"

	// AppframeworkManualUpdateConfigMap Config map for App Framework manual update
	AppframeworkManualUpdateConfigMap = "splunk-%s-manual-app-update"

	// DefaultStorageForAppDownloads is used to specify the default storage
	// for downloading apps on the operator pod
	DefaultStorageForAppDownloads = "10Gi"

	// DefaultStorageClassName is the storage class for PVC for downloading apps on operator
	DefaultStorageClassName = "gp2"

	// appDownlodPVCName is the name of PVC for downloading apps on operator
	appDownlodPVCName = "tmp-app-download"
	// ClusterMasterServiceName Cluster Manager Service Template String
	ClusterMasterServiceName = splcommon.TestClusterManager + "-service"

	// DeployerServiceName Cluster Manager Service Template String
	DeployerServiceName = "splunk-%s-shc-deployer-service"

	// CRUpdateRetryCount if CR Update fails retry these many time
	CRUpdateRetryCount = 10
)

var (
	metricsHost              = "0.0.0.0"
	metricsPort              = 8383
	specifiedOperatorImage   = defaultOperatorImage
	specifiedSplunkImage     = defaultSplunkImage
	specifiedSkipTeardown    = false
	specifiedLicenseFilePath = ""
	specifiedCommitHash      = ""
	// SpecifiedTestTimeout exported test timeout time as this can be
	// configured per test case if needed
	SpecifiedTestTimeout       = defaultTestTimeout
	installOperatorClusterWide = defaultOperatorInstallation
)

// OperatorFSGroup is the fsGroup value for Splunk Operator
var OperatorFSGroup int64 = 1001

//HTTPCodes Response codes for http request
var HTTPCodes = map[string]string{
	"Ok":           "HTTP/1.1 200 OK",
	"Forbidden":    "HTTP/1.1 403 Forbidden",
	"Unauthorized": "HTTP/1.1 401 Unauthorized",
}

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
	debug              string
}

func init() {
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	l := zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseFlagOptions(&opts))
	l.WithName("testenv")
	logf.SetLogger(l)

	flag.StringVar(&specifiedLicenseFilePath, "license-file", "", "Enterprise license file to use")
	flag.StringVar(&specifiedOperatorImage, "operator-image", defaultOperatorImage, "Splunk Operator image to use")
	flag.StringVar(&specifiedSplunkImage, "splunk-image", defaultSplunkImage, "Splunk Enterprise (splunkd) image to use")
	flag.BoolVar(&specifiedSkipTeardown, "skip-teardown", false, "True to skip tearing down the test env after use")
	flag.IntVar(&SpecifiedTestTimeout, "test-timeout", defaultTestTimeout, "Max test timeout in seconds to use")
	flag.StringVar(&specifiedCommitHash, "commit-hash", "", "commit hash string to use as part of the name")
	flag.StringVar(&installOperatorClusterWide, "cluster-wide", "true", "install operator clusterwide, if not install per test case")
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
		debug:              os.Getenv("DEBUG"),
	}

	testenv.Log = logf.Log.WithValues("testenv", testenv.name)

	// Scheme
	enterpriseApi.SchemeBuilder.AddToScheme(scheme.Scheme)

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	testenv.kubeAPIServer = cfg.Host
	testenv.Log.Info("Using kube-apiserver\n", "kube-apiserver", cfg.Host)

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

	return testenv, nil
}

//GetName returns the name of the testenv
func (testenv *TestEnv) GetName() string {
	return testenv.name
}

func (testenv *TestEnv) setup() error {
	testenv.Log.Info("testenv initializing.\n")
	testenv.initialized = true
	testenv.Log.Info("testenv initialized.\n", "namespace", testenv.namespace, "operatorImage", testenv.operatorImage, "splunkImage", testenv.splunkImage)
	return nil
}

// Teardown cleanup the resources use in this testenv
func (testenv *TestEnv) Teardown() error {

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
