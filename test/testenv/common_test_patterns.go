// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	corev1 "k8s.io/api/core/v1"
)

// ClusterCoordinator abstracts the v3/v4 API differences for cluster
// manager and license manager operations.
type ClusterCoordinator interface {
	LicenseManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error
	ClusterManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error
	DeployStandaloneWithLM(ctx context.Context, deployment *Deployment, name, mcRef string) (*enterpriseApi.Standalone, error)
	DeployMultisiteCluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, mcRef string) error
	DeployMultisiteClusterWithIndexes(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, secretName string, smartStoreSpec enterpriseApi.SmartStoreSpec) error
	VerifyClusterManagerPhaseUpdating(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error
	DeleteClusterManager(ctx context.Context, deployment *Deployment) error
	AppendSmartStoreIndex(ctx context.Context, deployment *Deployment, newIndex []enterpriseApi.IndexSpec) error
	GetBundleHash(ctx context.Context, deployment *Deployment) string
	ClusterManagerPVCType() string
	GetAPIVersion() string
}

// clusterMasterCoordinator implements ClusterCoordinator for v3 (ClusterMaster/LicenseMaster).
type clusterMasterCoordinator struct{}

func (c *clusterMasterCoordinator) LicenseManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyLicenseMasterReady(ctx, deployment)
}

func (c *clusterMasterCoordinator) ClusterManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyClusterMasterReady(ctx, deployment)
}

func (c *clusterMasterCoordinator) DeployStandaloneWithLM(ctx context.Context, deployment *Deployment, name, mcRef string) (*enterpriseApi.Standalone, error) {
	return deployment.DeployStandaloneWithLMaster(ctx, name, mcRef)
}

func (c *clusterMasterCoordinator) DeployMultisiteCluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, mcRef string) error {
	return deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
}

func (c *clusterMasterCoordinator) DeployMultisiteClusterWithIndexes(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, secretName string, smartStoreSpec enterpriseApi.SmartStoreSpec) error {
	return deployment.DeployMultisiteClusterMasterWithSearchHeadAndIndexes(ctx, name, indexerReplicas, siteCount, secretName, smartStoreSpec)
}

func (c *clusterMasterCoordinator) VerifyClusterManagerPhaseUpdating(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
}

func (c *clusterMasterCoordinator) DeleteClusterManager(ctx context.Context, deployment *Deployment) error {
	return GetAndDeleteCR(ctx, deployment, &enterpriseApiV3.ClusterMaster{}, deployment.GetName())
}

func (c *clusterMasterCoordinator) AppendSmartStoreIndex(ctx context.Context, deployment *Deployment, newIndex []enterpriseApi.IndexSpec) error {
	name := deployment.GetName()
	cm := &enterpriseApiV3.ClusterMaster{}
	if err := deployment.GetInstance(ctx, name, cm); err != nil {
		return fmt.Errorf("failed to get instance of Cluster Master: %w", err)
	}
	cm.Spec.SmartStore.IndexList = append(cm.Spec.SmartStore.IndexList, newIndex...)
	if err := deployment.UpdateCR(ctx, cm); err != nil {
		return fmt.Errorf("failed to add new index to Cluster Master: %w", err)
	}
	return nil
}

func (c *clusterMasterCoordinator) GetBundleHash(ctx context.Context, deployment *Deployment) string {
	return GetClusterManagerBundleHash(ctx, deployment, "ClusterMaster")
}

func (c *clusterMasterCoordinator) ClusterManagerPVCType() string {
	return "cluster-master"
}

func (c *clusterMasterCoordinator) GetAPIVersion() string {
	return "v3"
}

// clusterManagerCoordinator implements ClusterCoordinator for v4 (ClusterManager/LicenseManager).
type clusterManagerCoordinator struct{}

func (c *clusterManagerCoordinator) LicenseManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyLicenseManagerReady(ctx, deployment)
}

func (c *clusterManagerCoordinator) ClusterManagerReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyClusterManagerReady(ctx, deployment)
}

func (c *clusterManagerCoordinator) DeployStandaloneWithLM(ctx context.Context, deployment *Deployment, name, mcRef string) (*enterpriseApi.Standalone, error) {
	return deployment.DeployStandaloneWithLM(ctx, name, mcRef)
}

func (c *clusterManagerCoordinator) DeployMultisiteCluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, mcRef string) error {
	return deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
}

func (c *clusterManagerCoordinator) DeployMultisiteClusterWithIndexes(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, secretName string, smartStoreSpec enterpriseApi.SmartStoreSpec) error {
	return deployment.DeployMultisiteClusterWithSearchHeadAndIndexes(ctx, name, indexerReplicas, siteCount, secretName, smartStoreSpec)
}

func (c *clusterManagerCoordinator) VerifyClusterManagerPhaseUpdating(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
}

func (c *clusterManagerCoordinator) DeleteClusterManager(ctx context.Context, deployment *Deployment) error {
	return GetAndDeleteCR(ctx, deployment, &enterpriseApi.ClusterManager{}, deployment.GetName())
}

func (c *clusterManagerCoordinator) AppendSmartStoreIndex(ctx context.Context, deployment *Deployment, newIndex []enterpriseApi.IndexSpec) error {
	name := deployment.GetName()
	cm := &enterpriseApi.ClusterManager{}
	if err := deployment.GetInstance(ctx, name, cm); err != nil {
		return fmt.Errorf("failed to get instance of Cluster Manager: %w", err)
	}
	cm.Spec.SmartStore.IndexList = append(cm.Spec.SmartStore.IndexList, newIndex...)
	if err := deployment.UpdateCR(ctx, cm); err != nil {
		return fmt.Errorf("failed to add new index to Cluster Manager: %w", err)
	}
	return nil
}

func (c *clusterManagerCoordinator) GetBundleHash(ctx context.Context, deployment *Deployment) string {
	return GetClusterManagerBundleHash(ctx, deployment, "ClusterManager")
}

func (c *clusterManagerCoordinator) ClusterManagerPVCType() string {
	return "cluster-manager"
}

func (c *clusterManagerCoordinator) GetAPIVersion() string {
	return "v4"
}

// ClusterReadinessConfig embeds a ClusterCoordinator and provides composed
// deployment and verification workflows shared across test packages.
type ClusterReadinessConfig struct {
	ClusterCoordinator
}

// MasterManagerTestConfig pairs a name prefix and test label with a factory
// function that returns the appropriate ClusterReadinessConfig.
// This is the standard config type shared by test packages that loop over
// V3 (master) and V4 (manager) variants.
// See also MasterManagerLMTestConfig (in lmutil.go) for the license-manager
// equivalent that returns *LicenseTestConfig instead.
type MasterManagerTestConfig struct {
	NamePrefix string
	Label      string
	NewConfig  func() *ClusterReadinessConfig
}

// NewClusterReadinessConfigV3 creates a ClusterReadinessConfig for v3 API (LicenseMaster/ClusterMaster)
func NewClusterReadinessConfigV3() *ClusterReadinessConfig {
	return &ClusterReadinessConfig{ClusterCoordinator: &clusterMasterCoordinator{}}
}

// NewClusterReadinessConfigV4 creates a ClusterReadinessConfig for v4 API (LicenseManager/ClusterManager)
func NewClusterReadinessConfigV4() *ClusterReadinessConfig {
	return &ClusterReadinessConfig{ClusterCoordinator: &clusterManagerCoordinator{}}
}

// VerifyC3ClusterReady verifies the C3 cluster is ready using the config's ClusterManagerReady callback.
func (c *ClusterReadinessConfig) VerifyC3ClusterReady(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) error {
	return testcaseEnv.VerifyC3ClusterReady(ctx, deployment, func(ctx context.Context, d *Deployment) error {
		return c.ClusterManagerReady(ctx, d, testcaseEnv)
	})
}

// DeployMCAndGetVersion deploys and verifies a Monitoring Console, then returns both the MC
// instance and its current resource version.
func (testcaseenv *TestCaseEnv) DeployMCAndGetVersion(ctx context.Context, deployment *Deployment, name string, lmRef string) (*enterpriseApi.MonitoringConsole, string, error) {
	mc, err := testcaseenv.DeployAndVerifyMonitoringConsole(ctx, deployment, name, lmRef)
	if err != nil {
		return nil, "", err
	}
	resourceVersion := testcaseenv.GetResourceVersion(ctx, deployment, mc)
	return mc, resourceVersion, nil
}

// DeployAndVerifyStandalone deploys a standalone instance and verifies it reaches ready state
func (testcaseenv *TestCaseEnv) DeployAndVerifyStandalone(ctx context.Context, deployment *Deployment, mcRef string, licenseManagerRef string) (*enterpriseApi.Standalone, error) {
	name := deployment.GetName()
	standalone, err := deployment.DeployStandalone(ctx, name, mcRef, licenseManagerRef)
	if err != nil {
		return nil, fmt.Errorf("unable to deploy Standalone instance: %w", err)
	}
	if err := testcaseenv.VerifyStandaloneReady(ctx, deployment, name, standalone); err != nil {
		return nil, fmt.Errorf("standalone not ready: %w", err)
	}
	return standalone, nil
}

// DeployAndVerifyMonitoringConsole deploys a Monitoring Console and verifies it reaches ready state
func (testcaseenv *TestCaseEnv) DeployAndVerifyMonitoringConsole(ctx context.Context, deployment *Deployment, name string, licenseManagerRef string) (*enterpriseApi.MonitoringConsole, error) {
	mc, err := deployment.DeployMonitoringConsole(ctx, name, licenseManagerRef)
	if err != nil {
		return nil, fmt.Errorf("unable to deploy Monitoring Console instance: %w", err)
	}
	if err := testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, name, mc); err != nil {
		return nil, fmt.Errorf("monitoring console not ready: %w", err)
	}
	return mc, nil
}

// VerifyIndexerCPULimits verifies CPU limits on all indexer pods in a single-site cluster
func (testcaseenv *TestCaseEnv) VerifyIndexerCPULimits(deployment *Deployment, indexerCount int, expectedCPULimit string) error {
	for i := 0; i < indexerCount; i++ {
		podName := fmt.Sprintf(IndexerPod, deployment.GetName(), i)
		if err := testcaseenv.VerifyCPULimits(deployment, podName, expectedCPULimit); err != nil {
			return err
		}
	}
	return nil
}

// VerifySearchHeadCPULimits verifies CPU limits on all search head pods
func (testcaseenv *TestCaseEnv) VerifySearchHeadCPULimits(deployment *Deployment, searchHeadCount int, expectedCPULimit string) error {
	for i := 0; i < searchHeadCount; i++ {
		podName := fmt.Sprintf(SearchHeadPod, deployment.GetName(), i)
		if err := testcaseenv.VerifyCPULimits(deployment, podName, expectedCPULimit); err != nil {
			return err
		}
	}
	return nil
}

// VerifyM4ComponentsReady verifies the Cluster Manager, multisite indexers, multisite status, and SHC are ready.
func (testcaseenv *TestCaseEnv) VerifyM4ComponentsReady(ctx context.Context, deployment *Deployment, siteCount int, cmReadyFn func() error) error {
	if err := cmReadyFn(); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount); err != nil {
		return err
	}
	return testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyMCVersionChangedAndReady waits for the MC resource version to change then verifies MC is ready.
func (testcaseenv *TestCaseEnv) VerifyMCVersionChangedAndReady(ctx context.Context, deployment *Deployment, mc *enterpriseApi.MonitoringConsole, resourceVersion string) error {
	if err := testcaseenv.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion); err != nil {
		return err
	}
	return testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)
}

// VerifyClusterReadyAndRFSF is a V4-only verification pattern that checks C3 cluster is ready (using ClusterManager) and RF/SF is met
func (testcaseenv *TestCaseEnv) VerifyClusterReadyAndRFSF(ctx context.Context, deployment *Deployment) error {
	if err := testcaseenv.VerifyC3ClusterReady(ctx, deployment, testcaseenv.VerifyClusterManagerReady); err != nil {
		return err
	}
	return testcaseenv.VerifyRFSFMet(ctx, deployment)
}

// TriggerAndVerifyTelemetry is a common pattern for telemetry verification
func (testcaseenv *TestCaseEnv) TriggerAndVerifyTelemetry(ctx context.Context, deployment *Deployment, prevSubmissionTime string) error {
	testcaseenv.TriggerTelemetrySubmission(ctx, deployment)
	return testcaseenv.VerifyTelemetry(ctx, deployment, prevSubmissionTime)
}

// VerifyProbeConfigAndScripts verifies probe config map exists and probe scripts are present on all pods.
// If includeStartup is true, the startup probe script is also checked.
func (testcaseenv *TestCaseEnv) VerifyProbeConfigAndScripts(ctx context.Context, deployment *Deployment, includeStartup bool) error {
	testcaseenv.Log.Info("Get config map for livenessProbe and readinessProbe")
	configMapName := enterprise.GetProbeConfigMapName(testcaseenv.GetName())
	_, err := GetConfigMap(ctx, deployment, testcaseenv.GetName(), configMapName)
	if err != nil {
		return fmt.Errorf("unable to get config map for livenessProbe and readinessProbe %s: %w", configMapName, err)
	}
	scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName()}
	if includeStartup {
		scriptsNames = append(scriptsNames, enterprise.GetStartupScriptName())
	}
	allPods := DumpGetPods(testcaseenv.GetName())
	return testcaseenv.VerifyFilesInDirectoryOnPod(ctx, deployment, allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)
}

// NewStandaloneSpecWithMCRef creates a StandaloneSpec with a MonitoringConsoleRef set to the given MC name.
func NewStandaloneSpecWithMCRef(image string, mcName string) enterpriseApi.StandaloneSpec {
	return NewStandaloneSpecWithMCRefAndResources(image, mcName, corev1.ResourceRequirements{})
}

// NewStandaloneSpecWithMCRefAndResources creates a StandaloneSpec with a MonitoringConsoleRef
// and custom resource requirements.
func NewStandaloneSpecWithMCRefAndResources(image string, mcName string, resources corev1.ResourceRequirements) enterpriseApi.StandaloneSpec {
	return enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           image,
				Resources:       resources,
			},
			Volumes: []corev1.Volume{},
			MonitoringConsoleRef: corev1.ObjectReference{
				Name: mcName,
			},
		},
	}
}

// VerifyLMConfiguredOnMC verifies that the License Manager is configured on the Monitoring Console pod.
func VerifyLMConfiguredOnMC(ctx context.Context, deployment *Deployment) error {
	monitoringConsolePodName := fmt.Sprintf(MonitoringConsolePod, deployment.GetName())
	return VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)
}

// StandardC3Verification performs the standard V4-only set of verifications for a C3 cluster.
// This includes cluster ready (ClusterManager), RF/SF met, and monitoring console ready.
func (testcaseenv *TestCaseEnv) StandardC3Verification(ctx context.Context, deployment *Deployment, mc *enterpriseApi.MonitoringConsole) error {
	if err := testcaseenv.VerifyClusterReadyAndRFSF(ctx, deployment); err != nil {
		return err
	}
	return testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)
}

// DeployAndVerifyC3 deploys a C3 single-site cluster and verifies it reaches the ready state.
func (c *ClusterReadinessConfig) DeployAndVerifyC3(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv, replicas int, shc bool, mcRef string) error {
	if err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), replicas, shc, mcRef); err != nil {
		return fmt.Errorf("unable to deploy C3 cluster: %w", err)
	}
	return c.VerifyC3ClusterReady(ctx, deployment, testcaseEnv)
}

// DeployAndVerifyM4 deploys an M4 multisite cluster and verifies the Cluster Manager
// and all M4 components reach the ready state.
func (c *ClusterReadinessConfig) DeployAndVerifyM4(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv, indexerReplicas int, siteCount int, mcRef string) error {
	if err := c.DeployMultisiteCluster(ctx, deployment, deployment.GetName(), indexerReplicas, siteCount, mcRef); err != nil {
		return fmt.Errorf("unable to deploy M4 cluster: %w", err)
	}
	return testcaseEnv.VerifyM4ComponentsReady(ctx, deployment, siteCount, func() error {
		return c.ClusterManagerReady(ctx, deployment, testcaseEnv)
	})
}

// DeployC3WithLicense sets up the license config map, deploys a C3 cluster,
// and verifies both the License Manager and cluster reach the ready state.
func (c *ClusterReadinessConfig) DeployC3WithLicense(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv, replicas int, shc bool, mcRef string) error {
	if err := SetupLicenseConfigMap(ctx, testcaseEnv); err != nil {
		return err
	}
	if err := c.DeployAndVerifyC3(ctx, deployment, testcaseEnv, replicas, shc, mcRef); err != nil {
		return err
	}
	return c.LicenseManagerReady(ctx, deployment, testcaseEnv)
}

// DeployM4WithLicense sets up the license config map, deploys an M4 multisite cluster,
// and verifies the License Manager, Cluster Manager, and all M4 components reach the ready state.
func (c *ClusterReadinessConfig) DeployM4WithLicense(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv, indexerReplicas int, siteCount int, mcRef string) error {
	if err := SetupLicenseConfigMap(ctx, testcaseEnv); err != nil {
		return err
	}
	if err := c.DeployMultisiteCluster(ctx, deployment, deployment.GetName(), indexerReplicas, siteCount, mcRef); err != nil {
		return fmt.Errorf("unable to deploy M4 cluster: %w", err)
	}
	if err := c.LicenseManagerReady(ctx, deployment, testcaseEnv); err != nil {
		return err
	}
	return testcaseEnv.VerifyM4ComponentsReady(ctx, deployment, siteCount, func() error {
		return c.ClusterManagerReady(ctx, deployment, testcaseEnv)
	})
}
