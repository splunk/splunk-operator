package testenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//AppInfo Installation info on Apps
var AppInfo = map[string]map[string]string{
	"Splunk_SA_CIM":                     {"V1": "4.18.1", "V2": "4.19.0", "filename": "splunk-common-information-model-cim.tgz"},
	"TA-LDAP":                           {"V1": "4.0.0", "V2": "4.0.0", "filename": "add-on-for-ldap.tgz"},
	"DA-ESS-ContentUpdate":              {"V1": "3.20.0", "V2": "3.21.0", "filename": "splunk-es-content-update.tgz"},
	"Splunk_TA_paloalto":                {"V1": "6.6.0", "V2": "7.0.0", "filename": "palo-alto-networks-add-on-for-splunk.tgz"},
	"TA-MS-AAD":                         {"V1": "3.0.0", "V2": "3.1.1", "filename": "microsoft-azure-add-on-for-splunk.tgz"},
	"Splunk_TA_nix":                     {"V1": "8.3.0", "V2": "8.3.1", "filename": "splunk-add-on-for-unix-and-linux.tgz"},
	"splunk_app_microsoft_exchange":     {"V1": "4.0.1", "V2": "4.0.2", "filename": "splunk-app-for-microsoft-exchange.tgz"},
	"splunk_app_aws":                    {"V1": "6.0.1", "V2": "6.0.2", "filename": "splunk-app-for-aws.tgz"},
	"Splunk_ML_Toolkit":                 {"V1": "5.2.0", "V2": "5.2.1", "filename": "splunk-machine-learning-toolkit.tgz"},
	"Splunk_TA_microsoft-cloudservices": {"V1": "4.1.2", "V2": "4.1.3", "filename": "splunk-add-on-for-microsoft-cloud-services.tgz"},
	"splunk_app_stream":                 {"V1": "7.3.0", "V2": "7.4.0", "filename": "splunk-app-for-stream.tgz"},
	"Splunk_TA_stream_wire_data":        {"V1": "7.3.0", "V2": "7.4.0", "filename": "splunk-add-on-for-stream-wire-data.tgz"},
	"Splunk_TA_stream":                  {"V1": "7.3.0", "V2": "7.4.0", "filename": "splunk-add-on-for-stream-forwarders.tgz"},
	"splunk_app_db_connect":             {"V1": "3.5.0", "V2": "3.5.1", "filename": "splunk-db-connect.tgz"},
	"Splunk_Security_Essentials":        {"V1": "3.3.2", "V2": "3.3.3", "filename": "splunk-security-essentials.tgz"},
	"SplunkEnterpriseSecuritySuite":     {"V1": "6.4.0", "V2": "6.4.1", "filename": "splunk-enterprise-security.spl"},
}

//AppSourceInfo holds info related to app sources
type AppSourceInfo struct {
	CrKind                       string
	CrName                       string
	CrAppSourceName              string
	CrAppSourceNameLocal         string
	CrAppSourceNameCluster       string
	CrAppSourceVolumeName        string
	CrAppSourceVolumeNameLocal   string
	CrAppSourceVolumeNameCluster string
	CrPod                        []string
	CrAppScope                   string
	CrReplicas                   int
	CrSiteCount                  int
	CrMultisite                  bool
}

//BasicApps Apps that require no restart to be installed
var BasicApps = []string{"Splunk_SA_CIM", "DA-ESS-ContentUpdate", "Splunk_TA_paloalto", "TA-MS-AAD"}

//RestartNeededApps Apps that require restart to be installed
var RestartNeededApps = []string{"Splunk_TA_nix", "splunk_app_microsoft_exchange", "splunk_app_aws", "Splunk_ML_Toolkit", "Splunk_TA_microsoft-cloudservices", "splunk_app_stream", "Splunk_TA_stream", "Splunk_TA_stream_wire_data", "splunk_app_db_connect", "Splunk_Security_Essentials"}

//NewAppsAddedBetweenPolls Apps to be installed as poll after
var NewAppsAddedBetweenPolls = []string{"TA-LDAP"}

// AppLocationV1 Location of apps on S3 for V1 Apps
var AppLocationV1 = "appframework/v1apps/"

// AppLocationV2 Location of apps on S3 for V2 Apps
var AppLocationV2 = "appframework/v2apps/"

// AppStagingLocOnPod is the volume on Splunk pod where apps will be copied from operator
var AppStagingLocOnPod = "/operator-staging/appframework/"

// GenerateAppSourceSpec return AppSourceSpec struct with given values
func GenerateAppSourceSpec(appSourceName string, appSourceLocation string, appSourceDefaultSpec enterpriseApi.AppSourceDefaultSpec) enterpriseApi.AppSourceSpec {
	return enterpriseApi.AppSourceSpec{
		Name:                 appSourceName,
		Location:             appSourceLocation,
		AppSourceDefaultSpec: appSourceDefaultSpec,
	}
}

// GetPodAppStatus Get the app install status and version number
func GetPodAppStatus(deployment *Deployment, podName string, ns string, appname string, clusterWideInstall bool) (string, string, error) {
	// For clusterwide install do not check for versions on deployer and cluster-manager as the apps arent installed there
	if clusterWideInstall && (strings.Contains(podName, splcommon.TestClusterManagerDashed) || strings.Contains(podName, splcommon.TestDeployerDashed)) {
		logf.Log.Info("Pod skipped as install is Cluter-wide", "PodName", podName)
		return "", "", nil
	}
	output, err := GetPodAppInstallStatus(deployment, podName, ns, appname)
	if err != nil {
		return "", "", err
	}
	status := strings.Fields(output)[2]
	version, err := GetPodInstalledAppVersion(deployment, podName, ns, appname, clusterWideInstall)
	return status, version, err

}

// GetPodInstalledAppVersion Get the version of the app installed on pod
func GetPodInstalledAppVersion(deployment *Deployment, podName string, ns string, appname string, clusterWideInstall bool) (string, error) {
	path := "etc/apps"
	//For cluster-wide install the apps are extracted to different locations
	if clusterWideInstall {
		if strings.Contains(podName, "-indexer-") {
			path = splcommon.PeerAppsLoc
		} else if strings.Contains(podName, splcommon.ClusterManager) {
			path = splcommon.ManagerAppsLoc
		} else if strings.Contains(podName, splcommon.TestDeployerDashed) {
			path = splcommon.SHClusterAppsLoc
		}
	}
	filePath := fmt.Sprintf("/opt/splunk/%s/%s/default/app.conf", path, appname)
	logf.Log.Info("Check app version", "App", appname, "Conf file", filePath)

	confline, err := GetConfLineFromPod(podName, filePath, ns, "version", "launcher", true)
	if err != nil {
		logf.Log.Error(err, "Failed to get version from pod", "Pod Name", podName)
		return "", err
	}
	version := strings.TrimSpace(strings.Split(confline, "=")[1])
	return version, err

}

// GetPodAppInstallStatus Get the app install status
func GetPodAppInstallStatus(deployment *Deployment, podName string, ns string, appname string) (string, error) {
	stdin := fmt.Sprintf("/opt/splunk/bin/splunk display app '%s' -auth admin:$(cat /mnt/splunk-secrets/password)", appname)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin)
		return "", err
	}
	logf.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

	return strings.TrimSuffix(stdout, "\n"), nil
}

// GetPodAppbtoolStatus Get the app btool status
func GetPodAppbtoolStatus(deployment *Deployment, podName string, ns string, appname string) (string, error) {
	stdin := fmt.Sprintf("/opt/splunk/bin/splunk btool %s --app=%s --debug", appname, appname)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin)
		return "", err
	}
	logf.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

	if len(stdout) > 0 {
		if strings.Contains(strings.Split(stdout, "\n")[0], "App is disabled") {
			return "DISABLED", nil
		}
		return "ENABLED", nil
	}
	return "", err
}

// GetAppFileList Get the Versioned App file list for  app Names
func GetAppFileList(appList []string) []string {
	appFileList := make([]string, 0, len(appList))
	for _, app := range appList {
		appFileList = append(appFileList, AppInfo[app]["filename"])
	}
	return appFileList
}

// GetAppframeworkManualUpdateConfigMap gets config map for given manual update configmap
func GetAppframeworkManualUpdateConfigMap(deployment *Deployment, ns string) (*corev1.ConfigMap, error) {
	ConfigMapName := fmt.Sprintf(AppframeworkManualUpdateConfigMap, ns)
	logf.Log.Info("Get config map for", "CONFIG MAP NAME", ConfigMapName)
	ConfigMap, err := GetConfigMap(deployment, ns, ConfigMapName)
	if err != nil {
		logf.Log.Error(err, "Failed to get splunk manual poll Config Map")
		return ConfigMap, err
	}
	logf.Log.Info("Config Map contents", "CONFIG MAP NAME", ConfigMapName, "Data", ConfigMap.Data)
	return ConfigMap, err
}

// GetAppDeploymentInfoStandalone returns AppDeploymentInfo for given standalone, appSourceName and appName
func GetAppDeploymentInfoStandalone(deployment *Deployment, testenvInstance *TestEnv, name string, appSourceName string, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	standalone := &enterpriseApi.Standalone{}
	appDeploymentInfo := enterpriseApi.AppDeploymentInfo{}
	err := deployment.GetInstance(name, standalone)
	if err != nil {
		testenvInstance.Log.Error(err, "Failed to get CR ", "CR Name", name)
		return appDeploymentInfo, err
	}
	appInfoList := standalone.Status.AppContext.AppsSrcDeployStatus[appSourceName].AppDeploymentInfoList
	for _, appInfo := range appInfoList {
		testenvInstance.Log.Info("Checking Standalone AppInfo Struct", "App Name", appName, "App Source", appSourceName, "Standalone Name", name, "AppDeploymentInfo", appInfo)
		if strings.Contains(appName, appInfo.AppName) {
			testenvInstance.Log.Info("App Deployment Info found.", "App Name", appName, "App Source", appSourceName, "Standalone Name", name, "AppDeploymentInfo", appInfo)
			appDeploymentInfo = appInfo
			return appDeploymentInfo, nil
		}
	}
	testenvInstance.Log.Info("App Info not found in App Info List", "App Name", appName, "App Source", appSourceName, "Standalone Name", name, "App Info List", appInfoList)
	return appDeploymentInfo, err
}

// GetAppDeploymentInfoMonitoringConsole returns AppDeploymentInfo for given Monitoring Console, appSourceName and appName
func GetAppDeploymentInfoMonitoringConsole(deployment *Deployment, testenvInstance *TestEnv, name string, appSourceName string, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	mc := &enterpriseApi.MonitoringConsole{}
	appDeploymentInfo := enterpriseApi.AppDeploymentInfo{}
	err := deployment.GetInstance(name, mc)
	if err != nil {
		testenvInstance.Log.Error(err, "Failed to get CR ", "CR Name", name)
		return appDeploymentInfo, err
	}
	appInfoList := mc.Status.AppContext.AppsSrcDeployStatus[appSourceName].AppDeploymentInfoList
	for _, appInfo := range appInfoList {
		testenvInstance.Log.Info("Checking Monitoring Console AppInfo Struct", "App Name", appName, "App Source", appSourceName, "Monitoring Console Name", name, "AppDeploymentInfo", appInfo)
		if strings.Contains(appName, appInfo.AppName) {
			testenvInstance.Log.Info("App Deployment Info found.", "App Name", appName, "App Source", appSourceName, "Monitoring Console Name", name, "AppDeploymentInfo", appInfo)
			appDeploymentInfo = appInfo
			return appDeploymentInfo, nil
		}
	}
	testenvInstance.Log.Info("App Info not found in App Info List", "App Name", appName, "App Source", appSourceName, "Monitoring Console Name", name, "App Info List", appInfoList)
	return appDeploymentInfo, err
}

// GetAppDeploymentInfoClusterMaster returns AppDeploymentInfo for given Cluster Master, appSourceName and appName
func GetAppDeploymentInfoClusterMaster(deployment *Deployment, testenvInstance *TestEnv, name string, appSourceName string, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	cm := &enterpriseApi.ClusterMaster{}
	appDeploymentInfo := enterpriseApi.AppDeploymentInfo{}
	err := deployment.GetInstance(name, cm)
	if err != nil {
		testenvInstance.Log.Error(err, "Failed to get CR ", "CR Name", name)
		return appDeploymentInfo, err
	}
	appInfoList := cm.Status.AppContext.AppsSrcDeployStatus[appSourceName].AppDeploymentInfoList
	for _, appInfo := range appInfoList {
		testenvInstance.Log.Info("Checking Cluster Master AppInfo Struct", "App Name", appName, "App Source", appSourceName, "Cluster Master Name", name, "AppDeploymentInfo", appInfo)
		if strings.Contains(appName, appInfo.AppName) {
			testenvInstance.Log.Info("App Deployment Info found.", "App Name", appName, "App Source", appSourceName, "Cluster Master Name", name, "AppDeploymentInfo", appInfo)
			appDeploymentInfo = appInfo
			return appDeploymentInfo, nil
		}
	}
	testenvInstance.Log.Info("App Info not found in App Info List", "App Name", appName, "App Source", appSourceName, "Cluster Master Name", name, "App Info List", appInfoList)
	return appDeploymentInfo, err
}

// GetAppDeploymentInfoSearchHeadCluster returns AppDeploymentInfo for given Search Head Cluster, appSourceName and appName
func GetAppDeploymentInfoSearchHeadCluster(deployment *Deployment, testenvInstance *TestEnv, name string, appSourceName string, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	cm := &enterpriseApi.SearchHeadCluster{}
	appDeploymentInfo := enterpriseApi.AppDeploymentInfo{}
	err := deployment.GetInstance(name, cm)
	if err != nil {
		testenvInstance.Log.Error(err, "Failed to get CR ", "CR Name", name)
		return appDeploymentInfo, err
	}
	appInfoList := cm.Status.AppContext.AppsSrcDeployStatus[appSourceName].AppDeploymentInfoList
	for _, appInfo := range appInfoList {
		testenvInstance.Log.Info("Checking Search Head Cluster AppInfo Struct", "App Name", appName, "App Source", appSourceName, "Search Head Name Name", name, "AppDeploymentInfo", appInfo)
		if strings.Contains(appName, appInfo.AppName) {
			testenvInstance.Log.Info("App Deployment Info found.", "App Name", appName, "App Source", appSourceName, "Search Head Name Name", name, "AppDeploymentInfo", appInfo)
			appDeploymentInfo = appInfo
			return appDeploymentInfo, nil
		}
	}
	testenvInstance.Log.Info("App Info not found in App Info List", "App Name", appName, "App Source", appSourceName, "Search Head Name Name", name, "App Info List", appInfoList)
	return appDeploymentInfo, err
}

// GetAppDeploymentInfo returns AppDeploymentInfo for given CR Kind, appSourceName and appName
func GetAppDeploymentInfo(deployment *Deployment, testenvInstance *TestEnv, name string, crKind string, appSourceName string, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	var appDeploymentInfo enterpriseApi.AppDeploymentInfo
	var err error
	switch crKind {
	case "Standalone":
		appDeploymentInfo, err = GetAppDeploymentInfoStandalone(deployment, testenvInstance, name, appSourceName, appName)
	case "MonitoringConsole":
		appDeploymentInfo, err = GetAppDeploymentInfoMonitoringConsole(deployment, testenvInstance, name, appSourceName, appName)
	case "SearchHeadCluster":
		appDeploymentInfo, err = GetAppDeploymentInfoSearchHeadCluster(deployment, testenvInstance, name, appSourceName, appName)
	case "ClusterMaster":
		appDeploymentInfo, err = GetAppDeploymentInfoClusterMaster(deployment, testenvInstance, name, appSourceName, appName)
	default:
		message := fmt.Sprintf("Failed to fetch AppDeploymentInfo. Incorrect CR Kind %s", crKind)
		err = errors.New(message)
	}
	return appDeploymentInfo, err

}

// GenerateAppFrameworkSpec Generate Appframework spec
func GenerateAppFrameworkSpec(testenvInstance *TestEnv, volumeName string, scope string, appSourceName string, s3TestDir string, pollInterval int) enterpriseApi.AppFrameworkSpec {

	// Create App framework volume
	volumeSpec := []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: volumeName,
		Scope:   scope,
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceSpec := []enterpriseApi.AppSourceSpec{GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: int64(pollInterval),
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	return appFrameworkSpec
}

// WaitforPhaseChange Wait for 2 mins or when phase change on is seen on a CR for any particular app
func WaitforPhaseChange(deployment *Deployment, testenvInstance *TestEnv, name string, crKind string, appSourceName string, appList []string) {
	startTime := time.Now()

	for time.Since(startTime) <= time.Duration(2*time.Minute) {
		for _, appName := range appList {
			appDeploymentInfo, err := GetAppDeploymentInfo(deployment, testenvInstance, name, crKind, appSourceName, appName)
			if err != nil {
				testenvInstance.Log.Error(err, "Failed to get app deployment info")
			}
			if appDeploymentInfo.PhaseInfo.Phase != enterpriseApi.PhaseInstall {
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
}

// Verifications will perform several verifications needed between the different steps of App Framework tests
func Verifications(deployment *Deployment, testenvInstance *TestEnv, appSource []AppSourceInfo, appFileList []string, appFileList2 []string, appVersion string, appList []string, appList2 []string, splunkPodAge map[string]time.Time, status string, clusterManagerBundleHash string, scaling string) string {
	/* Function Steps
	 * Verify apps 'download' state for all CRs
	 * Verify apps 'podCopy' state for all CRs
	 * Verify apps packages are deleted from the operator pod for all CRs
	 * Verify apps 'install' state for all CRs
	 * Verify apps packages are deleted from the CR pods
	 * Verify bundle push is successful
	 * Verify apps are copied to correct location on CR pods
	 * Verify no pods did reset
	 * Verify apps are installed to correct location on CR pods
	 */
	// Verify apps 'Download' state for all CRs
	phase := enterpriseApi.PhaseDownload
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			testenvInstance.Log.Info("Verify apps 'download' state on Cluster Manager CR")
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" {
			testenvInstance.Log.Info("Verify apps 'download' state on Search Head Cluster CR")
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrKind == "Standalone" {
			testenvInstance.Log.Info("Verify apps 'download' state on Standalone CR")
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
		if appSource.CrKind == "MonitoringConsole" {
			testenvInstance.Log.Info("Verify apps 'download' state on Monitoring Console CR")
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
	}

	// Verify apps 'PodCopy' state for all CRs
	phase = enterpriseApi.PhasePodCopy
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			testenvInstance.Log.Info("Verify apps 'podCopy' state on Cluster Manager CR")
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" {
			testenvInstance.Log.Info("Verify apps 'podCopy' state on Search Head Cluster CR")
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrKind == "Standalone" {
			testenvInstance.Log.Info("Verify apps 'podCopy' state on Standalone CR")
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
		if appSource.CrKind == "MonitoringConsole" {
			testenvInstance.Log.Info("Verify apps 'podCopy' state on Monitoring Console CR")
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
	}

	// Verify apps packages are deleted from the operator pod for all CRs
	opPod := GetOperatorPodName(testenvInstance.GetName())
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps %s packages are deleted from the operator pod for Cluster Manager", appVersion))
			if appFileList2 != nil && appList2 != nil {
				opPathLocal := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceNameLocal)
				opPathCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeCluster, appSource.CrAppSourceNameCluster)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPathLocal)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList2, opPathCluster)
			} else if appSource.CrAppScope == "cluster" {
				opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeCluster, appSource.CrAppSourceName)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
			} else if appSource.CrAppScope == "local" {
				opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceName)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps %s packages are deleted from the operator pod for Search Head Cluster", appVersion))
			if appFileList2 != nil && appList2 != nil {
				opPathLocal := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceNameLocal)
				opPathCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeCluster, appSource.CrAppSourceNameCluster)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPathLocal)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList2, opPathCluster)
			} else if appSource.CrAppScope == "cluster" {
				opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeCluster, appSource.CrAppSourceName)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
			} else if appSource.CrAppScope == "local" {
				opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceName)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
			}
		}
		if appSource.CrKind == "Standalone" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps %s packages are deleted from the operator pod for Standalone", appVersion))
			opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceName)
			VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
		}
		if appSource.CrKind == "MonitoringConsole" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps %s packages are deleted from the operator pod for Monitoring Console", appVersion))
			opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), enterpriseApi.ScopeLocal, appSource.CrAppSourceName)
			VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opPath)
		}
	}

	// Verify apps 'install' state for all CRs
	phase = enterpriseApi.PhaseInstall
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps 'install' state on Cluster Manager CR", appVersion))
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrName == "SearchHeadCluster" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps 'install' state on Search Head Cluster CR", appVersion))
			if appFileList2 != nil && appList2 != nil {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameLocal, phase, appFileList)
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceNameCluster, phase, appFileList2)
			} else {
				VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
			}
		}
		if appSource.CrName == "Standalone" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps 'install' state on Standalone CR", appVersion))
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
		if appSource.CrName == "MonitoringConsole" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps 'install' state on Monitoring Console CR", appVersion))
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appFileList)
		}
	}

	// Verify apps packages are deleted from the CR pods
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			clusterManagerPodName := fmt.Sprintf(ClusterManagerPod, deployment.GetName())
			if appFileList2 != nil && appList2 != nil {
				cmLocalDownload := "/init-apps/" + appSource.CrAppSourceVolumeNameLocal
				cmClusterDownload := "/init-apps/" + appSource.CrAppSourceVolumeNameCluster
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, cmLocalDownload)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList2, cmClusterDownload)
			} else {
				cmDownloadLocation := "/init-apps/" + appSource.CrAppSourceVolumeName
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, cmDownloadLocation)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" {
			deployerPodName := fmt.Sprintf(DeployerPod, deployment.GetName())
			if appFileList2 != nil && appList2 != nil {
				shcLocalDownload := "/init-apps/" + appSource.CrAppSourceVolumeNameLocal
				shcClusterDownload := "/init-apps/" + appSource.CrAppSourceVolumeNameCluster
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Deployer pod %s", appVersion, deployerPodName))
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, shcLocalDownload)
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList2, shcClusterDownload)
			} else {
				shcDownloadLocation := "/init-apps/" + appSource.CrAppSourceVolumeName
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Deployer pod %s", appVersion, deployerPodName))
				VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, shcDownloadLocation)
			}
		}
		if appSource.CrName == "Standalone" {
			standaloneLocalDownload := "/init-apps/" + appSource.CrAppSourceName
			standalonePodName := fmt.Sprintf(StandalonePod, appSource.CrName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Standalone pod %s", appVersion, standalonePodName))
			VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, standaloneLocalDownload)
		}
		if appSource.CrName == "MonitoringConsole" {
			mcLocalDownload := "/init-apps/" + appSource.CrAppSourceName
			mcPodName := fmt.Sprintf(MonitoringConsolePod, appSource.CrName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, mcLocalDownload)
		}
	}

	// Verify bundle push status
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			if status == "bundle_save" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify Cluster Manager bundle push status (%s apps)", appVersion))
				VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas, "")
			} else if status == "bundle_compare" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify Cluster Manager bundle push status (%s apps) and compare bundle hash with previous bundle hash", appVersion))
				VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas, clusterManagerBundleHash)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" {
			if status == "bundle_save" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify Deployer bundle push status (%s apps)", appVersion))
				VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas)
			} else if status == "bundle_compare" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify Deployer bundle push status (%s apps) and compare bundle hash with previous bundle hash", appVersion))
				VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas)
			}
		}
	}
	// Saving current bundle hash for future comparison
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" {
			if status == "bundle_save" {
				testenvInstance.Log.Info("Saving current bundle hash for future comparison")
				clusterManagerBundleHash = GetClusterManagerBundleHash(deployment)
			} else {
				clusterManagerBundleHash = ""
			}
		}
	}

	// Verify apps are copied to correct location on all CRs
	if appFileList2 == nil && appList2 == nil {
		for _, appSource := range appSource {
			if appSource.CrKind == "ClusterMaster" && appSource.CrAppScope == "local" {
				allPodNames := []string{fmt.Sprintf(ClusterManagerPod, deployment.GetName())}
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are copied to /etc/apps/ on Cluster Manager", appVersion))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, true, false)
			}
			if appSource.CrKind == "SearchHeadCluster" && appSource.CrAppScope == "local" {
				allPodNames := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are copied to /etc/apps/ on Deployer", appVersion))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, true, false)
			}
			if appSource.CrKind == "ClusterMaster" && appSource.CrAppScope == "cluster" {
				allPodNames := []string{fmt.Sprintf(ClusterManagerPod, deployment.GetName())}
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are NOT copied to /etc/apps/ on Cluster Manager (App List: %s)", appVersion, appFileList))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, false, false)
				allPodNames = append(allPodNames, GeneratePodNameSlice(IndexerPod, deployment.GetName(), appSource.CrReplicas, false, 1)...)
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are copied to /etc/apps/ on Indexers", appVersion))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, true, true)
			}
			if appSource.CrKind == "SearchHeadCluster" && appSource.CrAppScope == "cluster" {
				allPodNames := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are NOT copied to /etc/apps/ on Deployer (App List: %s) ", appVersion, appFileList))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, false, false)
				allPodNames = append(allPodNames, GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), appSource.CrReplicas, false, 1)...)
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are copied to /etc/apps/ on Search Heads", appVersion))
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appList, true, true)
			}
			if appSource.CrKind == "Standalone" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are copied to /etc/apps/ on Standalone", appVersion))
				standalonePodName := fmt.Sprintf(StandalonePod, appSource.CrName, 0)
				if scaling == "up" {
					podNames := []string{standalonePodName}
					podNames = append(podNames, fmt.Sprintf(StandalonePod, deployment.GetName(), 1))
					VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appList, true, false)
				} else {
					VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appList, true, false)
				}
			}
			if appSource.CrKind == "MonitoringConsole" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are copied to /etc/apps/ on Monitoring Console", appVersion))
				mcPodName := fmt.Sprintf(MonitoringConsolePod, appSource.CrName, 0)
				VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appList, true, false)
			}
		}
	}

	// Verify no pods reset by checking the pod age
	testenvInstance.Log.Info("Verify no pods reset by checking the pod age")
	if scaling == "up" || scaling == "down" {
		for _, appSource := range appSource {
			if appSource.CrKind == "Standalone" {
				// Excluding MC pod from list of pods to verify at it will reset after scaling
				mcPodName := fmt.Sprintf(MonitoringConsolePod, appSource.CrName, 0)
				VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge, []string{mcPodName})
			}
			if appSource.CrKind == "SearchHeadCluster" {
				// Excluding SHC pods from list of pods to verify at they will reset after scaling
				shcPodNames := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
				shcPodNames = append(shcPodNames, GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), appSource.CrReplicas, false, 1)...)
				VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge, shcPodNames)
			}
		}
	} else {
		VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge, nil)
	}

	// Verify apps are installed at correct location on the pods
	var checkupdated bool
	if appVersion == "V1" {
		checkupdated = false
	} else if appVersion == "V2" {
		checkupdated = true
	}
	if appFileList2 != nil && appList2 != nil {
		for _, appSource := range appSource {
			if appSource.CrKind == "ClusterMaster" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are installed locally on Cluster Manager", appVersion))
				cmPod := []string{fmt.Sprintf(ClusterManagerPod, deployment.GetName())}
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), cmPod, appList, true, "enabled", checkupdated, false)
				clusterPodNames := []string{}
				clusterPodNames = append(clusterPodNames, GeneratePodNameSlice(IndexerPod, deployment.GetName(), appSource.CrReplicas, false, 1)...)
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are installed on Indexers", appVersion))
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appList2, true, "enabled", checkupdated, true)
			} else if appSource.CrKind == "SearchHeadCluster" {
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are installed locally on Deployer", appVersion))
				deployerPod := []string{fmt.Sprintf(DeployerPod, deployment.GetName())}
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, appList, true, "enabled", checkupdated, false)
				shcPodNames := []string{}
				shcPodNames = append(shcPodNames, GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), appSource.CrReplicas, false, 1)...)
				testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are installed on Search Heads", appVersion))
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), shcPodNames, appList2, true, "enabled", checkupdated, true)
			}
		}
	}
	// Verify apps are installed on Standalone
	for _, appSource := range appSource {
		if appSource.CrKind == "Standalone" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Standalone", appVersion))
			standalonePodName := fmt.Sprintf(StandalonePod, appSource.CrName, 0)
			if scaling == "up" {
				podNames := []string{standalonePodName}
				podNames = append(podNames, fmt.Sprintf(StandalonePod, deployment.GetName(), 1))
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appList, true, "enabled", checkupdated, false)
			} else {
				VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appList, true, "enabled", checkupdated, false)
			}
		}
	}
	// Verify apps are installed on Monitoring Console
	for _, appSource := range appSource {
		if appSource.CrKind == " MonitoringConsole" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console", appVersion))
			mcPodName := fmt.Sprintf(MonitoringConsolePod, appSource.CrName, 0)
			VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appList, true, "enabled", checkupdated, false)
		}
	}
	return clusterManagerBundleHash
}
