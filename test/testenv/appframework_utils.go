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
	"test_app":                          {"V1": "1.0.0", "V2": "1.0.0", "filename": "test_app.tgz"},
	"test_app2":                         {"V1": "1.0.0", "V2": "1.0.0", "filename": "test_app2.tgz"},
	"test_app3":                         {"V1": "1.0.0", "V2": "1.0.0", "filename": "test_app3.tgz"},
}

//AppSourceInfo holds info related to app sources
type AppSourceInfo struct {
	CrKind                string
	CrName                string
	CrAppSourceName       string
	CrAppSourceVolumeName string
	CrPod                 []string
	CrAppVersion          string
	CrAppScope            string
	CrAppList             []string
	CrAppFileList         []string
	CrReplicas            int
	CrSiteCount           int
	CrMultisite           bool
	CrClusterPods         []string
}

//BasicApps Apps that require no restart to be installed
var BasicApps = []string{"Splunk_SA_CIM", "DA-ESS-ContentUpdate", "Splunk_TA_paloalto", "TA-MS-AAD"}

//RestartNeededApps Apps that require restart to be installed
var RestartNeededApps = []string{"Splunk_TA_nix", "splunk_app_microsoft_exchange", "splunk_app_aws", "Splunk_ML_Toolkit", "Splunk_TA_microsoft-cloudservices", "splunk_app_stream", "Splunk_TA_stream", "Splunk_TA_stream_wire_data", "splunk_app_db_connect", "Splunk_Security_Essentials"}

//NewAppsAddedBetweenPolls Apps to be installed as poll after
var NewAppsAddedBetweenPolls = []string{"TA-LDAP"}

//BigSingleApp is 1 app with bigger size to perform tests while installation is in progress
var BigSingleApp = []string{"test_app"}

//ExtraApps is 2 apps to be added to app source during app installa in progress
var ExtraApps = []string{"test_app2", "test_app3"}

// AppLocationV1 Location of apps on S3 for V1 Apps
var AppLocationV1 = "appframework/v1apps/"

// AppLocationV2 Location of apps on S3 for V2 Apps
var AppLocationV2 = "appframework/v2apps/"

// AppLocationDisabledApps Location of apps on S3 for Disabled Apps
var AppLocationDisabledApps = "appframework/Disabledapps/"

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
	volumeSpec := []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3", GetDefaultS3Region())}

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

// AppFrameWorkVerifications will perform several verifications needed between the different steps of App Framework tests
func AppFrameWorkVerifications(deployment *Deployment, testenvInstance *TestEnv, appSource []AppSourceInfo, splunkPodAge map[string]time.Time, clusterManagerBundleHash string) string {
	/* Function Steps
	 * Verify apps 'download' and 'podCopy' states for all CRs
	 * Verify apps packages are deleted from the operator pod for all CRs
	 * Verify apps 'install' state for all CRs
	 * Verify apps packages are deleted from the CR pods
	 * Verify bundle push status
	 * Verify apps are copied to correct location on CR pods
	 * Verify apps are installed to correct location on CR pods
	 */

	// Verify apps 'download' and 'podCopy' states for all CRs
	for _, phase := range []enterpriseApi.AppPhaseType{enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy} {
		for _, appSource := range appSource {
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps '%v' state on CR %v with name %v", phase, appSource.CrKind, appSource.CrName))
			VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, phase, appSource.CrAppFileList)
		}
	}

	// Verify apps packages are deleted from the operator pod for all CRs
	opPod := GetOperatorPodName(testenvInstance.GetName())
	for _, appSource := range appSource {
		testenvInstance.Log.Info(fmt.Sprintf("Verify apps %s packages are deleted from the operator pod for CR %v with name %v", appSource.CrAppVersion, appSource.CrKind, appSource.CrName))
		opPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), appSource.CrKind, deployment.GetName(), appSource.CrAppScope, appSource.CrAppSourceName)
		VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appSource.CrAppFileList, opPath)
	}

	// Verify apps 'install' state for all CRs
	for _, appSource := range appSource {
		testenvInstance.Log.Info(fmt.Sprintf("Verify apps '%v' state on CR %v with name %v", enterpriseApi.PhaseInstall, appSource.CrKind, appSource.CrName))
		VerifyAppListPhase(deployment, testenvInstance, appSource.CrName, appSource.CrKind, appSource.CrAppSourceName, enterpriseApi.PhaseInstall, appSource.CrAppFileList)
	}

	// Verify apps packages are deleted from the CR pods
	for _, appSource := range appSource {
		podDownloadPath := AppStagingLocOnPod + appSource.CrAppSourceVolumeName
		pod := appSource.CrPod
		testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps packages are deleted on pod %s", appSource.CrAppVersion, pod))
		VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), pod, appSource.CrAppFileList, podDownloadPath)
	}

	// Verify bundle push status
	for _, appSource := range appSource {
		if appSource.CrKind == "ClusterMaster" && appSource.CrAppScope == enterpriseApi.ScopeCluster {
			testenvInstance.Log.Info(fmt.Sprintf("Verify Cluster Manager bundle push status (%s apps) and compare bundle hash with previous bundle hash", appSource.CrAppVersion))
			VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas, clusterManagerBundleHash)
			if clusterManagerBundleHash == "" {
				clusterManagerBundleHash = GetClusterManagerBundleHash(deployment)
			}
		}
		if appSource.CrKind == "SearchHeadCluster" && appSource.CrAppScope == enterpriseApi.ScopeCluster {
			testenvInstance.Log.Info(fmt.Sprintf("Verify Deployer bundle push status (%s apps)", appSource.CrAppVersion))
			VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrReplicas)
		}
	}

	// Verify apps are copied to correct location on all CRs
	for _, appSource := range appSource {
		if appSource.CrAppScope == enterpriseApi.ScopeLocal {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope are copied to /etc/apps/ for CR %s with name %s", appSource.CrAppVersion, appSource.CrKind, appSource.CrName))
			VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrPod, appSource.CrAppList, true, appSource.CrAppScope)
		} else {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are NOT copied to /etc/apps/ on %v pod", appSource.CrAppVersion, appSource.CrPod))
			VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrPod, appSource.CrAppList, false, appSource.CrAppScope)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope are copied on %v pods", appSource.CrAppVersion, appSource.CrClusterPods))
			VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), appSource.CrClusterPods, appSource.CrAppList, true, appSource.CrAppScope)
		}
	}

	// Verify apps are installed at correct location on the pods
	for _, appSource := range appSource {
		allPodNames := appSource.CrPod
		checkUpdated := appSource.CrAppVersion == "V2"
		if appSource.CrAppScope == "local" {
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'local' scope for CR %s with name %s are installed on pod %s", appSource.CrAppVersion, appSource.CrKind, appSource.CrName, allPodNames))
			VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appSource.CrAppList, true, "enabled", checkUpdated, false)
		} else {
			allPodNames = appSource.CrClusterPods
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with 'cluster' scope for CR %s with name %s are installed on pods %s", appSource.CrAppVersion, appSource.CrKind, appSource.CrName, allPodNames))
			VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appSource.CrAppList, true, "enabled", checkUpdated, true)
		}
	}
	return clusterManagerBundleHash
}
