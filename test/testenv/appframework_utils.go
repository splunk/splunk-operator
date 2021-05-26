package testenv

import (
	"fmt"
	"strings"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// GenerateAppSourceSpec return AppSourceSpec struct with given values
func GenerateAppSourceSpec(appSourceName string, appSourceLocation string, appSourceDefaultSpec enterprisev1.AppSourceDefaultSpec) enterprisev1.AppSourceSpec {
	return enterprisev1.AppSourceSpec{
		Name:                 appSourceName,
		Location:             appSourceLocation,
		AppSourceDefaultSpec: appSourceDefaultSpec,
	}
}

// GetAppStatus Get the app install status and enabled status
func GetAppStatus(deployment *Deployment, podName string, ns string, appname string) (string, string, error) {
	filePath := fmt.Sprintf("/opt/splunk/etc/apps/%s/default/app.conf", appname)
	confline, err := GetConfLineFromPod(podName, filePath, ns, "state", "install", true)
	if err != nil {
		logf.Log.Error(err, "Failed to get install status from pod", "Pod Name", podName)
		return "", "", err
	}
	status := strings.TrimSpace(strings.Split(confline, "=")[1])
	confline, err = GetConfLineFromPod(podName, filePath, ns, "Version", "", false)
	if err != nil {
		logf.Log.Error(err, "Failed to get Version from pod", "Pod Name", podName)
		return "", "", err
	}
	version := strings.TrimSpace(strings.Split(confline, " ")[2])
	return status, version, err

}
