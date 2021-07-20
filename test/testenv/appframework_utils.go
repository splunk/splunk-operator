package testenv

import (
	"fmt"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//AppInfo Installation info on Apps
var AppInfo = map[string]map[string]string{
	"Splunk_SA_CIM":                     {"V1": "4.18.1", "V2": "4.19.0", "V2filename": "splunk-common-information-model-cim_4190.tgz", "V1filename": "splunk-common-information-model-cim_4181.tgz"},
	"TA-LDAP":                           {"V1": "4.0.0", "V2": "4.0.0", "V2filename": "add-on-for-ldap_400.tgz", "V1filename": "add-on-for-ldap_400.tgz"},
	"DA-ESS-ContentUpdate":              {"V1": "3.20.0", "V2": "3.21.0", "V2filename": "splunk-es-content-update_3210.tgz", "V1filename": "splunk-es-content-update_3200.tgz"},
	"Splunk_TA_paloalto":                {"V1": "6.6.0", "V2": "7.0.0", "V2filename": "palo-alto-networks-add-on-for-splunk_700.tgz", "V1filename": "palo-alto-networks-add-on-for-splunk_660.tgz"},
	"TA-MS-AAD":                         {"V1": "3.0.0", "V2": "3.1.1", "V2filename": "microsoft-azure-add-on-for-splunk_311.tgz", "V1filename": "microsoft-azure-add-on-for-splunk_300.tgz"},
	"Splunk_TA_nix":                     {"V1": "8.3.0", "V2": "8.3.0", "V2filename": "splunk-add-on-for-unix-and-linux_830.tgz", "V1filename": "splunk-add-on-for-unix-and-linux_830.tgz"},
	"splunk_app_microsoft_exchange":     {"V1": "4.0.1", "V2": "4.0.2", "V2filename": "splunk-app-for-microsoft-exchange_402.tgz", "V1filename": "splunk-app-for-microsoft-exchange_401.tgz"},
	"splunk_app_aws":                    {"V1": "6.0.1", "V2": "6.0.2", "V2filename": "Splunk-app-for-aws_602.tgz", "V1filename": "Splunk-app-for-aws_601.tgz"},
	"Splunk_ML_Toolkit":                 {"V1": "5.2.0", "V2": "5.2.1", "V2filename": "splunk-machine-learning-toolkit_521.tgz", "V1filename": "splunk-machine-learning-toolkit_520.tgz"},
	"Splunk_TA_microsoft-cloudservices": {"V1": "4.1.2", "V2": "4.1.3", "V2filename": "splunk-add-on-for-microsoft-cloud-services_413.tgz", "V1filename": "splunk-add-on-for-microsoft-cloud-services_412.tgz"},
	"splunk_app_stream":                 {"V1": "6.0.1", "V2": "6.0.2", "V2filename": "Splunk-app-for-stream_720.tgz", "V1filename": "Splunk-app-for-stream_720.tgz"},
	"splunk_app_db_connect":             {"V1": "3.5.0", "V2": "3.5.1", "V2filename": "Splunk-db-connect_351.tgz", "V1filename": "Splunk-db-connect_350.tgz"},
	"Splunk_Security_Essentials":        {"V1": "3.3.2", "V2": "3.3.3", "V2filename": "Splunk-security-essentials_333.tgz", "V1filename": "Splunk-security-essentials_332.tgz"},
	"SplunkEnterpriseSecuritySuite":     {"V1": "6.4.0", "V2": "6.4.1", "V2filename": "splunk-enterprise-security_641.spl", "V1filename": "splunk-enterprise-security_640.spl"},
}

//BasicApps Apps that require no restart to be installed
var BasicApps = []string{"Splunk_SA_CIM", "DA-ESS-ContentUpdate", "Splunk_TA_paloalto", "TA-MS-AAD"}

//RestartNeededApps Apps that require restart to be installed
var RestartNeededApps = []string{"Splunk_TA_nix", "splunk_app_microsoft_exchange", "splunk_app_aws", "Splunk_ML_Toolkit", "Splunk_TA_microsoft-cloudservices", "splunk_app_stream", "splunk_app_db_connect", "Splunk_Security_Essentials"}

//NewAppsAddedBetweenPolls Apps to be installed as poll after
var NewAppsAddedBetweenPolls = []string{"TA-LDAP"}

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
	// For clusterwide install do not check for versions on deployer and cluster-master as the apps arent installed there
	if clusterWideInstall && (strings.Contains(podName, "-cluster-master-") || strings.Contains(podName, "-deployer-")) {
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
			path = "etc/slave-apps"
		} else if strings.Contains(podName, "cluster-master") {
			path = "etc/master-apps"
		} else if strings.Contains(podName, "-deployer-") {
			path = "etc/shcluster/apps"
		}
	}
	filePath := fmt.Sprintf("/opt/splunk/%s/%s/default/app.conf", path, appname)
	logf.Log.Info("Check Version for app", "AppName", appname, "config", filePath)

	confline, err := GetConfLineFromPod(podName, filePath, ns, "version", "launcher", true)
	if err != nil {
		logf.Log.Error(err, "Failed to get Version from pod", "Pod Name", podName)
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
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

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
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

	if len(stdout) > 0 {
		if strings.Contains(strings.Split(stdout, "\n")[0], "Application is disabled") {
			return "DISABLED", nil
		}
		return "ENABLED", nil
	}
	return "", err
}

// GetAppFileList Get the Versioned App file list for  app Names
func GetAppFileList(appList []string, version int) []string {
	fileKey := fmt.Sprintf("V%dfilename", version)
	appFileList := make([]string, 0, len(appList))
	for _, app := range appList {
		appFileList = append(appFileList, AppInfo[app][fileKey])
	}
	return appFileList
}
