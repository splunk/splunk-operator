package c3appfw

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

// C3AppFrameworkUpgradeTest deploys a C3 SVA with App Framework enabled,
// installs V1 apps (with MC), then upgrades them to V2 and re-verifies.
// This is the shared flow behind the "upgrade" smoke test in every cloud provider.
func C3AppFrameworkUpgradeTest(
	ctx context.Context,
	backend testenv.CloudStorageBackend,
	testcaseEnvInst *testenv.TestCaseEnv,
	deployment *testenv.Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V1"
	appFileList := testenv.GetAppFileList(appListV1)

	// Upload V1 apps for Monitoring Console
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	testDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
	uploadedFiles, err := backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
	appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
	appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, testDirMC, 60)

	mcSpec := enterpriseApi.MonitoringConsoleSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseEnvInst.GetSplunkImage(),
			},
			Volumes: []corev1.Volume{},
		},
		AppFrameworkConfig: appFrameworkSpecMC,
	}

	testcaseEnvInst.Log.Info("Deploy Monitoring Console")
	mcName := deployment.GetName()
	mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Upload V1 apps for Indexer Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	testDirIdxc := "c3appfw-idxc-" + testenv.RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V1 apps for Search Head Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	testDirShc := "c3appfw-shc-" + testenv.RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
	appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, testDirShc, 60)

	resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	indexerReplicas := 3
	shReplicas := 3
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)
	testenv.VerifyNoDisconnectedSHPresentOnCM(ctx, deployment, testcaseEnvInst)

	splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
	ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
	_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
	Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
	scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
	allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
	testenv.VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)

	// Initial verifications
	idxcPodNames := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
	mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
	cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
	allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Upgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	appVersion = "V2"
	appFileList = testenv.GetAppFileList(appListV2)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	// Final verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV2
	cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV2
	shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
	mcAppSourceInfo.CrAppVersion = appVersion
	mcAppSourceInfo.CrAppList = appListV2
	mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
	allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}

// C3AppFrameworkDowngradeTest deploys a C3 SVA with App Framework enabled,
// installs V2 apps (with MC), then downgrades them to V1 and re-verifies.
func C3AppFrameworkDowngradeTest(
	ctx context.Context,
	backend testenv.CloudStorageBackend,
	testcaseEnvInst *testenv.TestCaseEnv,
	deployment *testenv.Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V2"
	appFileList := testenv.GetAppFileList(appListV2)

	// Upload V2 apps for Monitoring Console
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	testDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
	uploadedFiles, err := backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
	appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
	appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, testDirMC, 60)

	mcSpec := enterpriseApi.MonitoringConsoleSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseEnvInst.GetSplunkImage(),
			},
			Volumes: []corev1.Volume{},
		},
		AppFrameworkConfig: appFrameworkSpecMC,
	}

	testcaseEnvInst.Log.Info("Deploy Monitoring Console")
	mcName := deployment.GetName()
	mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Upload V2 apps for Indexer Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	testDirIdxc := "c3appfw-idxc-" + testenv.RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V2 apps for Search Head Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	testDirShc := "c3appfw-shc-" + testenv.RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
	appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, testDirShc, 60)

	resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	indexerReplicas := 3
	shReplicas := 3
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	// Initial verifications
	idxcPodNames := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
	mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
	cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
	allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Downgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	appVersion = "V1"
	appFileList = testenv.GetAppFileList(appListV1)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexers", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexers", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	// Final verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV1
	cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV1
	shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
	mcAppSourceInfo.CrAppVersion = appVersion
	mcAppSourceInfo.CrAppList = appListV1
	mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
	allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}

// C3AppFrameworkLocalScopeUpgradeTest deploys a C3 SVA with App Framework
// enabled using local scope, installs V1 apps, then upgrades them to V2.
func C3AppFrameworkLocalScopeUpgradeTest(
	ctx context.Context,
	backend testenv.CloudStorageBackend,
	testcaseEnvInst *testenv.TestCaseEnv,
	deployment *testenv.Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V1"
	appFileList := testenv.GetAppFileList(appListV1)

	// Upload V1 apps for Indexer Cluster
	testDirIdxc := "c3appfw-idxc-" + testenv.RandomDNSName(4)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	uploadedFiles, err := backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V1 apps for Search Head Cluster
	testDirShc := "c3appfw-shc-" + testenv.RandomDNSName(4)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
	appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, testDirShc, 60)

	// Deploy C3 CRD
	indexerReplicas := 3
	shReplicas := 3
	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	// Initial verifications
	idxcPodNames := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
	cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
	testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Upgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	appVersion = "V2"
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps", appVersion))
	appFileList = testenv.GetAppFileList(appListV2)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

	// Upgrade verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV2
	cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV2
	shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
	allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
	testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}
