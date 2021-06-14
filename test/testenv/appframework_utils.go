package testenv

import (
	"fmt"
	"strings"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//AppInfo Installation info on Apps
var AppInfo = map[string]map[string]string{
	"splunk-common-information-model-cim_4181.tgz":       {"App-name": "Splunk_SA_CIM", "V1": "4.18.1", "V2": "4.19.0", "V2filename": "splunk-common-information-model-cim_4190.tgz"},
	"Add-on-for-ldap_400.tgz":                            {"App-name": "TA-LDAP", "V1": "4.0.0", "V2": "4.0.0", "V2filename": "Add-on-for-ldap_400.tgz"},
	"splunk-es-content-update_3200.tgz":                  {"App-name": "DA-ESS-ContentUpdate", "V1": "3.20.0", "V2": "3.21.0", "V2filename": "splunk-es-content-update_3210.tgz"},
	"palo-alto-networks-add-on-for-splunk_660.tgz":       {"App-name": "Splunk_TA_paloalto", "V1": "6.6.0", "V2": "7.0.0", "V2filename": "palo-alto-networks-add-on-for-splunk_700.tgz"},
	"microsoft-azure-add-on-for-splunk_300.tgz":          {"App-name": "TA-MS-AAD", "V1": "3.0.0", "V2": "3.1.1", "V2filename": "microsoft-azure-add-on-for-splunk_311.tgz"},
	"splunk-add-on-for-unix-and-linux_830.tgz":           {"App-name": "Splunk_TA_nix", "V1": "8.3.0", "V2": "8.3.0", "V2filename": "splunk-add-on-for-unix-and-linux_830.tgz"},
	"splunk-app-for-microsoft-exchange_401.tgz":          {"App-name": "splunk_app_microsoft_exchange", "V1": "4.0.1", "V2": "4.0.2", "V2filename": "splunk-app-for-microsoft-exchange_402.tgz"},
	"Splunk-app-for-aws_601.tgz":                         {"App-name": "splunk_app_aws", "V1": "6.0.1", "V2": "6.0.2", "V2filename": "Splunk-app-for-aws_602.tgz"},
	"splunk-machine-learning-toolkit_520.tgz":            {"App-name": "Splunk_ML_Toolkit", "V1": "5.2.0", "V2": "5.2.1", "V2filename": "splunk-machine-learning-toolkit_521.tgz"},
	"splunk-add-on-for-microsoft-cloud-services_412.tgz": {"App-name": "Splunk_TA_microsoft-cloudservices", "V1": "4.1.2", "V2": "4.1.3", "V2filename": "splunk-add-on-for-microsoft-cloud-services_413.tgz"},
	"Splunk-app-for-stream_720.tgz":                      {"App-name": "splunk_app_stream", "V1": "6.0.1", "V2": "6.0.2", "V2filename": "Splunk-app-for-stream_720.tgz"},
	"Splunk-db-connect_350.tgz":                          {"App-name": "splunk_app_db_connect", "V1": "3.5.0", "V2": "3.5.1", "V2filename": "Splunk-db-connect_351.tgz"},
	"Splunk-security-essentials_332.tgz":                 {"App-name": "Splunk_Security_Essentials", "V1": "3.3.2", "V2": "3.3.3", "V2filename": "Splunk-security-essentials_333.tgz"},
	//Need to explore these apps for their app Name and installation steps
	"splunk-app-for-vmware_401.tgz":          {"App-name": "", "V1": "4.0.1", "V2": "4.0.2", "V2filename": "splunk-app-for-vmware_402.tgz"},
	"splunk-it-service-intelligence_472.spl": {"App-name": "", "V1": "4.7.2", "V2": "4.9.0", "V2filename": "splunk-it-service-intelligence_490.spl"},
	"splunk-enterprise-security_640.spl":     {"App-name": "", "V1": "6.4.0", "V2": "6.4.1", "V2filename": "splunk-enterprise-security_641.spl"},
}

//BasicApps Apps that require no restart to be installed
var BasicApps = []string{"splunk-common-information-model-cim_4181.tgz", "splunk-es-content-update_3200.tgz", "palo-alto-networks-add-on-for-splunk_660.tgz", "microsoft-azure-add-on-for-splunk_300.tgz"}

//RestartNeededApps Apps that require restart to be installed
var RestartNeededApps = []string{"splunk-add-on-for-unix-and-linux_830.tgz", "splunk-app-for-microsoft-exchange_401.tgz", "Splunk-app-for-aws_601.tgz", "splunk-machine-learning-toolkit_520.tgz", "splunk-add-on-for-microsoft-cloud-services_412.tgz", "Splunk-app-for-stream_720.tgz", "Splunk-db-connect_350.tgz", "Splunk-security-essentials_332.tgz"}

//AdditionalStepsApps Apps that require additional steps to be installed
var AdditionalStepsApps = []string{"splunk-app-for-vmware_401.tgz", "splunk-it-service-intelligence_472.spl", "splunk-enterprise-security_640.spl"}

// GenerateAppSourceSpec return AppSourceSpec struct with given values
func GenerateAppSourceSpec(appSourceName string, appSourceLocation string, appSourceDefaultSpec enterprisev1.AppSourceDefaultSpec) enterprisev1.AppSourceSpec {
	return enterprisev1.AppSourceSpec{
		Name:                 appSourceName,
		Location:             appSourceLocation,
		AppSourceDefaultSpec: appSourceDefaultSpec,
	}
}

// GetPodAppStatus Get the app install status and version number
func GetPodAppStatus(deployment *Deployment, podName string, ns string, appname string) (string, string, error) {
	output, _ := GetPodAppInstallStatus(deployment, podName, ns, appname)
	status := strings.Fields(output)[2]
	version, err := GetPodInstalledAppVersion(deployment, podName, ns, appname)
	return status, version, err

}

// GetPodInstalledAppVersion Get the version of the app installed on pod
func GetPodInstalledAppVersion(deployment *Deployment, podName string, ns string, appname string) (string, error) {
	path := "etc/apps"
	if strings.Contains(podName, "-indexer-") {
		path = "etc/slave-apps/"
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
	//Get password to via secret object
	namespaceScopedSecretName := fmt.Sprintf(NamespaceScopedSecretObjectName, ns)
	secretStruct, _ := GetSecretStruct(deployment, ns, namespaceScopedSecretName)
	logf.Log.Info("Data in secret object", "data", secretStruct.Data)
	pwd := DecryptSplunkEncodedSecret(deployment, podName, ns, string(secretStruct.Data["password"]))

	//Use password to access info
	stdin := fmt.Sprintf("/opt/splunk/bin/splunk display app '%s' -auth admin:%s", appname, pwd)
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
