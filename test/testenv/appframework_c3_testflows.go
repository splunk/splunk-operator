package testenv

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	corev1 "k8s.io/api/core/v1"
)

// C3AppFrameworkUpgradeTest deploys a C3 SVA with App Framework enabled,
// installs V1 apps (with MC), then upgrades them to V2 and re-verifies.
// This is the shared flow behind the "upgrade" smoke test in every cloud provider.
func C3AppFrameworkUpgradeTest(
	ctx context.Context,
	backend CloudStorageBackend,
	testcaseEnvInst *TestCaseEnv,
	deployment *Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V1"
	appFileList := GetAppFileList(appListV1)

	// Upload V1 apps for Monitoring Console
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	testDirMC := "c3appfw-mc-" + RandomDNSName(4)
	uploadedFiles, err := backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + RandomDNSName(3)
	appSourceVolumeNameMC := "appframework-test-volume-mc-" + RandomDNSName(3)
	appFrameworkSpecMC := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, testDirMC, 60)

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

	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Upload V1 apps for Indexer Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	testDirIdxc := "c3appfw-idxc-" + RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V1 apps for Search Head Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	testDirShc := "c3appfw-shc-" + RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + RandomDNSName(3)
	appFrameworkSpecIdxc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, testDirShc, 60)

	resourceVersion := GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	indexerReplicas := 3
	shReplicas := 3
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)
	VerifyNoDisconnectedSHPresentOnCM(ctx, deployment, testcaseEnvInst)

	splunkPodAge := GetPodsStartTime(testcaseEnvInst.GetName())

	testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
	ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
	_, err = GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
	Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
	scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
	allPods := DumpGetPods(testcaseEnvInst.GetName())
	VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)

	// Initial verifications
	idxcPodNames := GeneratePodNameSlice(IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
	mcPod := []string{fmt.Sprintf(MonitoringConsolePod, deployment.GetName())}
	cmAppSourceInfo := AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	mcAppSourceInfo := AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
	allAppSourceInfo := []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	ClusterMasterBundleHash := AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Upgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	resourceVersion = GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	appVersion = "V2"
	appFileList = GetAppFileList(appListV2)
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

	WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge = GetPodsStartTime(testcaseEnvInst.GetName())

	// Final verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV2
	cmAppSourceInfo.CrAppFileList = GetAppFileList(appListV2)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV2
	shcAppSourceInfo.CrAppFileList = GetAppFileList(appListV2)
	mcAppSourceInfo.CrAppVersion = appVersion
	mcAppSourceInfo.CrAppList = appListV2
	mcAppSourceInfo.CrAppFileList = GetAppFileList(appListV2)
	allAppSourceInfo = []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}

// C3AppFrameworkDowngradeTest deploys a C3 SVA with App Framework enabled,
// installs V2 apps (with MC), then downgrades them to V1 and re-verifies.
func C3AppFrameworkDowngradeTest(
	ctx context.Context,
	backend CloudStorageBackend,
	testcaseEnvInst *TestCaseEnv,
	deployment *Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V2"
	appFileList := GetAppFileList(appListV2)

	// Upload V2 apps for Monitoring Console
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Monitoring Console", appVersion))
	testDirMC := "c3appfw-mc-" + RandomDNSName(4)
	uploadedFiles, err := backend.UploadFiles(ctx, testDirMC, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Monitoring Console", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + RandomDNSName(3)
	appSourceVolumeNameMC := "appframework-test-volume-mc-" + RandomDNSName(3)
	appFrameworkSpecMC := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, testDirMC, 60)

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

	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Upload V2 apps for Indexer Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	testDirIdxc := "c3appfw-idxc-" + RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V2 apps for Search Head Cluster
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	testDirShc := "c3appfw-shc-" + RandomDNSName(4)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + RandomDNSName(3)
	appFrameworkSpecIdxc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, testDirShc, 60)

	resourceVersion := GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	indexerReplicas := 3
	shReplicas := 3
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge := GetPodsStartTime(testcaseEnvInst.GetName())

	// Initial verifications
	idxcPodNames := GeneratePodNameSlice(IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
	mcPod := []string{fmt.Sprintf(MonitoringConsolePod, deployment.GetName())}
	cmAppSourceInfo := AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	mcAppSourceInfo := AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
	allAppSourceInfo := []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	ClusterMasterBundleHash := AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Downgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	resourceVersion = GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

	appVersion = "V1"
	appFileList = GetAppFileList(appListV1)
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

	WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
	VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	splunkPodAge = GetPodsStartTime(testcaseEnvInst.GetName())

	// Final verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV1
	cmAppSourceInfo.CrAppFileList = GetAppFileList(appListV1)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV1
	shcAppSourceInfo.CrAppFileList = GetAppFileList(appListV1)
	mcAppSourceInfo.CrAppVersion = appVersion
	mcAppSourceInfo.CrAppList = appListV1
	mcAppSourceInfo.CrAppFileList = GetAppFileList(appListV1)
	allAppSourceInfo = []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
	AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}

// C3AppFrameworkLocalScopeUpgradeTest deploys a C3 SVA with App Framework
// enabled using local scope, installs V1 apps, then upgrades them to V2.
func C3AppFrameworkLocalScopeUpgradeTest(
	ctx context.Context,
	backend CloudStorageBackend,
	testcaseEnvInst *TestCaseEnv,
	deployment *Deployment,
	appListV1, appListV2 []string,
	downloadDirV1, downloadDirV2 string,
	uploadedApps *[]string,
) {
	appVersion := "V1"
	appFileList := GetAppFileList(appListV1)

	// Upload V1 apps for Indexer Cluster
	testDirIdxc := "c3appfw-idxc-" + RandomDNSName(4)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Indexer Cluster", appVersion))
	uploadedFiles, err := backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	// Upload V1 apps for Search Head Cluster
	testDirShc := "c3appfw-shc-" + RandomDNSName(4)
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps for Search Head Cluster", appVersion))
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV1)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeLocal + RandomDNSName(3)
	appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeLocal + RandomDNSName(3)
	appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + RandomDNSName(3)
	appSourceVolumeNameShc := "appframework-test-volume-shc-" + RandomDNSName(3)
	appFrameworkSpecIdxc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, testDirIdxc, 60)
	appFrameworkSpecShc := GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, testDirShc, 60)

	// Deploy C3 CRD
	indexerReplicas := 3
	shReplicas := 3
	testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
	cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
	Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	splunkPodAge := GetPodsStartTime(testcaseEnvInst.GetName())

	// Initial verifications
	idxcPodNames := GeneratePodNameSlice(IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
	shcPodNames := GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
	cmPod := []string{fmt.Sprintf(ClusterMasterPod, deployment.GetName())}
	deployerPod := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
	cmAppSourceInfo := AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
	shcAppSourceInfo := AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
	allAppSourceInfo := []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
	AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

	// Upgrade apps
	testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on cloud storage", appVersion))
	backend.DeleteFiles(ctx, *uploadedApps)
	*uploadedApps = nil

	appVersion = "V2"
	testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps", appVersion))
	appFileList = GetAppFileList(appListV2)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirIdxc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Indexer Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)
	uploadedFiles, err = backend.UploadFiles(ctx, testDirShc, appFileList, downloadDirV2)
	Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps for Search Head Cluster", appVersion))
	*uploadedApps = append(*uploadedApps, uploadedFiles...)

	WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

	ClusterMasterReady(ctx, deployment, testcaseEnvInst)
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	splunkPodAge = GetPodsStartTime(testcaseEnvInst.GetName())

	// Upgrade verifications
	cmAppSourceInfo.CrAppVersion = appVersion
	cmAppSourceInfo.CrAppList = appListV2
	cmAppSourceInfo.CrAppFileList = GetAppFileList(appListV2)
	shcAppSourceInfo.CrAppVersion = appVersion
	shcAppSourceInfo.CrAppList = appListV2
	shcAppSourceInfo.CrAppFileList = GetAppFileList(appListV2)
	allAppSourceInfo = []AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
	AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

	VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
}
