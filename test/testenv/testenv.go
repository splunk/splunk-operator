// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"net"
	"os"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
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
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	defaultOperatorInstallation = "false"

	defaultOperatorImage = "splunk/splunk-operator"
	defaultSplunkImage   = "splunk/splunk:latest"

	// PollInterval specifies the polling interval for slow operations (waiting for full cluster readiness)
	PollInterval = 5 * time.Second

	// ShortPollInterval specifies the polling interval for fast-transitioning states
	ShortPollInterval = 2 * time.Second

	// ConsistentPollInterval is the interval to use to consistently check a state is stable
	ConsistentPollInterval = 200 * time.Millisecond

	// ConsistentDuration is use to check a state is stable
	ConsistentDuration = 2000 * time.Millisecond

	// SearchHeadPod Template String for search head pod
	SearchHeadPod = "splunk-%s-shc-search-head-%d"

	// DeployerPod Template String for deployer pod
	DeployerPod = "splunk-%s-shc-deployer-0"

	// StandalonePod Template String for standalone pod
	StandalonePod = "splunk-%s-standalone-%d"

	// LicenseManagerPod Template String for License Manager pod
	LicenseManagerPod = "splunk-%s-license-manager-%d"

	// LicenseMasterPod Template String for License Master pod
	LicenseMasterPod = "splunk-%s-" + splcommon.LicenseManager + "-%d"

	// IngestorPod Template String for ingestor pod
	IngestorPod = "splunk-%s-ingestor-%d"

	// IndexerPod Template String for indexer pod
	IndexerPod = "splunk-%s-idxc-indexer-%d"

	// PVCString Template String for PVC
	PVCString = "pvc-%s-splunk-%s-%s-%d"

	// MonitoringConsoleSts Monitoring Console Statefulset Template
	MonitoringConsoleSts = "splunk-%s-monitoring-console"

	// MonitoringConsolePod Monitoring Console Pod Template String
	MonitoringConsolePod = "splunk-%s-monitoring-console-0"

	// ClusterManagerPod ClusterManager Pod Template String
	ClusterManagerPod = "splunk-%s-cluster-manager-0"

	// ClusterMasterPod ClusterMaster Pod Template String
	ClusterMasterPod = "splunk-%s-" + splcommon.ClusterManager + "-0"

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
	// ClusterManagerServiceName Cluster Manager Service Template String
	ClusterManagerServiceName = "splunk-%s-cluster-manager-service"
	// ClusterMasterServiceName Cluster Master Service Template String
	ClusterMasterServiceName = "splunk-%s-cluster-master-service"

	// DeployerServiceName Deployer Service Template String
	DeployerServiceName = "splunk-%s-shc-deployer-service"

	// CRUpdateRetryCount if CR Update fails retry these many time
	CRUpdateRetryCount = 10

	// LogLineCount is the default number of log lines to ingest for test data
	LogLineCount = 2000

	// DefaultIngestIndex is the default index name used for test data ingestion
	DefaultIngestIndex = "main"
)

var (
	metricsHost                 = "0.0.0.0"
	metricsPort                 = 8383
	specifiedOperatorImage      = defaultOperatorImage
	specifiedSplunkImage        = defaultSplunkImage
	specifiedSplunkUpgradeImage = ""
	specifiedSkipTeardown       = false
	specifiedLicenseFilePath    = ""
	specifiedCommitHash         = ""
	specifiedJobID              = ""
	// SpecifiedTestTimeout exported test timeout time as this can be
	// configured per test case if needed
	SpecifiedTestTimeout       = defaultTestTimeout
	installOperatorClusterWide = defaultOperatorInstallation
)

// Label keys applied to every test namespace at creation time.
//
// SokSmokeJobLabel (value = CI_JOB_ID) is used for bulk post-job namespace
// cleanup on existing-cluster jobs (e.g. FIPS lane) where the cluster is not
// torn down between runs:
//
//	kubectl delete ns -l sok-smoke-job=<CI_JOB_ID> --wait=false
//
// On ephemeral EKS jobs the entire cluster is deleted post-run, so this label
// is informational only (useful for kubectl debugging during a live run).
//
// SokSmokeSuiteLabel (value = <testenv-name>-<CI_JOB_ID>, e.g. "4d9f-s1appfw-xyz-12345678")
// is a finer-grained label that uniquely identifies a single suite instance
// within a job.  Two parallel specs from different suites (S1 and C3) in the
// same job get different SokSmokeSuiteLabel values, so a targeted suite
// cleanup does not touch sibling suites.
const (
	SokSmokeJobLabel   = "sok-smoke-job"
	SokSmokeSuiteLabel = "sok-smoke-suite"
)

// OperatorFSGroup is the fsGroup value for Splunk Operator
var OperatorFSGroup int64 = 1001

// HTTPCodes Response codes for http request
var HTTPCodes = map[string]string{
	"Ok":           "HTTP/1.1 200 OK",
	"Forbidden":    "HTTP/1.1 403 Forbidden",
	"Unauthorized": "HTTP/1.1 401 Unauthorized",
}

type cleanupFunc func() error

// TestEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run tests against
type TestEnv struct {
	kubeAPIServer        string
	name                 string
	namespace            string
	serviceAccountName   string
	roleName             string
	roleBindingName      string
	operatorName         string
	operatorImage        string
	splunkImage          string
	splunkUpgradeImage   string
	initialized          bool
	SkipTeardown         bool
	licenseFilePath      string
	licenseCMName        string
	s3IndexSecret        string
	indexIngestSepSecret string
	kubeClient           client.Client
	Log                  logr.Logger
	cleanupFuncs         []cleanupFunc
	debug                string
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
	if os.Getenv("GRAVITON_TESTING") == "true" {
		flag.StringVar(&specifiedSplunkImage, "splunk-image", os.Getenv("SPLUNK_ENTERPRISE_IMAGE"), "Splunk Enterprise (splunkd) image to use")
	} else {
		flag.StringVar(&specifiedSplunkImage, "splunk-image", defaultSplunkImage, "Splunk Enterprise (splunkd) image to use")
	}
	flag.BoolVar(&specifiedSkipTeardown, "skip-teardown", false, "True to skip tearing down the test env after use")
	flag.IntVar(&SpecifiedTestTimeout, "test-timeout", defaultTestTimeout, "Max test timeout in seconds to use")
	flag.StringVar(&specifiedSplunkUpgradeImage, "splunk-upgrade-image", "", "Splunk Enterprise image to upgrade to for rolling update tests")
	flag.StringVar(&specifiedCommitHash, "commit-hash", "", "commit hash string to use as part of the name")
	flag.StringVar(&specifiedJobID, "job-id", os.Getenv("CI_JOB_ID"), "CI job ID used to label test namespaces for isolated post-job cleanup")
	flag.StringVar(&installOperatorClusterWide, "cluster-wide", "true", "install operator clusterwide, if not install per test case")
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// NewDefaultTestEnv creates a default test environment
func NewDefaultTestEnv(name string) (*TestEnv, error) {
	if os.Getenv("GRAVITON_TESTING") == "true" {
		return NewTestEnv(name, specifiedCommitHash, specifiedOperatorImage, os.Getenv("SPLUNK_ENTERPRISE_IMAGE"), specifiedLicenseFilePath)
	} else {
		return NewTestEnv(name, specifiedCommitHash, specifiedOperatorImage, specifiedSplunkImage, specifiedLicenseFilePath)
	}
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
		name:                 envName,
		namespace:            envName,
		serviceAccountName:   envName,
		roleName:             envName,
		roleBindingName:      envName,
		operatorName:         "splunk-op-" + envName,
		operatorImage:        operatorImage,
		splunkImage:          splunkImage,
		splunkUpgradeImage:   specifiedSplunkUpgradeImage,
		SkipTeardown:         specifiedSkipTeardown,
		licenseCMName:        envName,
		licenseFilePath:      licenseFilePath,
		s3IndexSecret:        "splunk-s3-index-" + envName,
		indexIngestSepSecret: "splunk--index-ingest-sep-" + name,
		debug:                os.Getenv("DEBUG"),
	}

	testenv.Log = logf.Log.WithValues("testenv", testenv.name)

	// Scheme
	enterpriseApi.SchemeBuilder.AddToScheme(scheme.Scheme)
	enterpriseApiV3.SchemeBuilder.AddToScheme(scheme.Scheme)

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	testenv.kubeAPIServer = cfg.Host
	testenv.Log.Info("Using kube-apiserver\n", "kube-apiserver", cfg.Host)

	suiteConfig, _ := ginkgo.GinkgoConfiguration()

	metricsAddr := fmt.Sprintf("%s:%d", metricsHost, metricsPort+suiteConfig.ParallelProcess)

	kubeManager, err := manager.New(cfg, manager.Options{
		Metrics: server.Options{
			BindAddress:  metricsAddr,
			ListenConfig: net.ListenConfig{},
		},
		Scheme: scheme.Scheme,
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
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "Error starting kube manager")
	}()

	return testenv, nil
}

// GetName returns the name of the testenv
func (testenv *TestEnv) GetName() string {
	return testenv.name
}

// GetSplunkImage returns the Splunk Enterprise image configured for this testenv.
func (testenv *TestEnv) GetSplunkImage() string {
	return testenv.splunkImage
}

// GetSplunkUpgradeImage returns the Splunk Enterprise upgrade image for rolling update tests.
// Falls back to splunkImage if no upgrade image is configured.
func (testenv *TestEnv) GetSplunkUpgradeImage() string {
	if testenv.splunkUpgradeImage != "" {
		return testenv.splunkUpgradeImage
	}
	return testenv.splunkImage
}

// HasLicenseFile returns true when a license file path is configured, meaning
// LicenseManager deployment is expected in cluster-topology tests.
func (testenv *TestEnv) HasLicenseFile() bool {
	return testenv.licenseFilePath != ""
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
