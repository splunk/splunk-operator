package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
)

const (
	requeAfterInSeconds = 21600 // Send telemetry once every 6 hour
	defaultTestMode     = "false"
	defaultTestVersion  = "3.1.0"

	telStatusKey     = "status"
	telDeploymentKey = "deployment"
	cpuRequestKey    = "cpu_request"
	memoryRequestKey = "memory_request"
	cpuLimitKey      = "cpu_limit"
	memoryLimitKey   = "memory_limit"
)

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

type Telemetry struct {
	Type          string                 `json:"type"`
	Component     string                 `json:"component"`
	OptInRequired int                    `json:"optInRequired"`
	Data          map[string]interface{} `json:"data"`
	Test          bool                   `json:"test"`
	Visibility    string                 `json:"visibility,omitempty"`
}

type TelemetryStatus struct {
	LastTransmission string `json:"lastTransmission,omitempty"`
	Test             string `json:"test,omitempty"`
	SokVersion       string `json:"sokVersion,omitempty"`
}

func ApplyTelemetry(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 10 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * requeAfterInSeconds,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyTelemetry")

	for k, _ := range cm.Data {
		logger.InfoContext(ctx, "Retrieved telemetry keys", "key", k)
	}

	var data map[string]interface{}
	data = make(map[string]interface{})

	currentStatus := getCurrentStatus(ctx, cm)
	// Add SOK version
	data[telSOKVersionKey] = currentStatus.SokVersion
	var telDeployment map[string]interface{}
	telDeployment = make(map[string]interface{})
	data[telDeploymentKey] = telDeployment
	// Add SOK telemetry
	crWithTelAppList := collectDeploymentTelData(ctx, client, telDeployment)
	/*
	 * Add other component's telemetry set in splunk-operator-manager-telemetry configmap.
	 * i.e splunk POD's telemetry
	 */
	CollectCMTelData(ctx, cm, data)

	// Now send the telemetry
	for _, crs := range crWithTelAppList {
		for _, cr := range crs {
			test := false
			if currentStatus.Test == "true" {
				test = true
			}
			success := SendTelemetry(ctx, client, cr, data, test)
			if success {
				updateLastTransmissionTime(ctx, client, cm, currentStatus)
				return result, nil
			}
		}
	}

	return result, errors.New("Failed to send telemetry data")
}

func updateLastTransmissionTime(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap, status *TelemetryStatus) {
	logger := logging.FromContext(ctx).With("func", "updateLastTransmissionTime")

	status.LastTransmission = time.Now().UTC().Format(time.RFC3339)
	updated, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal telemetry status", "error", err)
		return
	}
	cm.Data[telStatusKey] = string(updated)
	if err = client.Update(ctx, cm); err != nil {
		logger.ErrorContext(ctx, "Failed to update telemetry status in configmap", "error", err)
		return
	}
	logger.InfoContext(ctx, "Updated last transmission time in configmap", "newStatus", cm.Data[telStatusKey])
}

func collectResourceTelData(resources corev1.ResourceRequirements) map[string]string {
	retData := make(map[string]string)
	defaultResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(defaultRequestsCPU),
			corev1.ResourceMemory: resource.MustParse(defaultRequestsMemory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(defaultLimitsCPU),
			corev1.ResourceMemory: resource.MustParse(defaultLimitsMemory),
		},
	}

	if resources.Requests == nil {
		cpu := defaultResources.Requests[corev1.ResourceCPU]
		mem := defaultResources.Requests[corev1.ResourceMemory]
		retData[cpuRequestKey] = (&cpu).String()
		retData[memoryRequestKey] = (&mem).String()
	} else {
		if cpuReq, ok := resources.Requests[corev1.ResourceCPU]; ok {
			retData[cpuRequestKey] = cpuReq.String()
		} else {
			cpu := defaultResources.Requests[corev1.ResourceCPU]
			retData[cpuRequestKey] = (&cpu).String()
		}
		if memReq, ok := resources.Requests[corev1.ResourceMemory]; ok {
			retData[memoryRequestKey] = memReq.String()
		} else {
			mem := defaultResources.Requests[corev1.ResourceMemory]
			retData[memoryRequestKey] = (&mem).String()
		}
	}

	if resources.Limits == nil {
		cpu := defaultResources.Limits[corev1.ResourceCPU]
		mem := defaultResources.Limits[corev1.ResourceMemory]
		retData[cpuLimitKey] = (&cpu).String()
		retData[memoryLimitKey] = (&mem).String()
	} else {
		if cpuLim, ok := resources.Limits[corev1.ResourceCPU]; ok {
			retData[cpuLimitKey] = cpuLim.String()
		} else {
			cpu := defaultResources.Limits[corev1.ResourceCPU]
			retData[cpuLimitKey] = (&cpu).String()
		}
		if memLim, ok := resources.Limits[corev1.ResourceMemory]; ok {
			retData[memoryLimitKey] = memLim.String()
		} else {
			mem := defaultResources.Limits[corev1.ResourceMemory]
			retData[memoryLimitKey] = (&mem).String()
		}
	}
	return retData
}

type crListHandler struct {
	kind        string
	handlerFunc func(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error)
	checkTelApp bool
}

func collectDeploymentTelData(ctx context.Context, client splcommon.ControllerClient, deploymentData map[string]interface{}) map[string][]splcommon.MetaObject {
	logger := logging.FromContext(ctx).With("func", "collectDeploymentTelData")

	var crWithTelAppList map[string][]splcommon.MetaObject
	crWithTelAppList = make(map[string][]splcommon.MetaObject)

	logger.InfoContext(ctx, "Start collecting deployment telemetry data")
	// Define all CR handlers in a slice
	handlers := []crListHandler{
		{kind: "Standalone", handlerFunc: handleStandalones, checkTelApp: true},
		{kind: "LicenseManager", handlerFunc: handleLicenseManagers, checkTelApp: true},
		{kind: "LicenseMaster", handlerFunc: handleLicenseMasters, checkTelApp: true},
		{kind: "SearchHeadCluster", handlerFunc: handleSearchHeadClusters, checkTelApp: true},
		{kind: "IndexerCluster", handlerFunc: handleIndexerClusters, checkTelApp: false},
		{kind: "ClusterManager", handlerFunc: handleClusterManagers, checkTelApp: true},
		{kind: "ClusterMaster", handlerFunc: handleClusterMasters, checkTelApp: true},
		{kind: "MonitoringConsole", handlerFunc: handleMonitoringConsoles, checkTelApp: false},
	}

	// Process each CR type using the same logic
	for _, handler := range handlers {
		data, crs, err := handler.handlerFunc(ctx, client)
		if err != nil {
			logger.ErrorContext(ctx, "Error processing CR type", "error", err, "kind", handler.kind)
			continue
		}
		if handler.checkTelApp && crs != nil && len(crs) > 0 {
			crWithTelAppList[handler.kind] = crs
		}
		if data != nil {
			deploymentData[handler.kind] = data
		}
	}

	logger.InfoContext(ctx, "Successfully collected deployment telemetry data", "deploymentData", deploymentData)
	return crWithTelAppList
}

func handleStandalones(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.StandaloneList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleLicenseManagers(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.LicenseManagerList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleLicenseMasters(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApiV3.LicenseMasterList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleSearchHeadClusters(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.SearchHeadClusterList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleIndexerClusters(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.IndexerClusterList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	for i := range list.Items {
		cr := &list.Items[i]
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, nil, nil
}

func handleClusterManagers(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.ClusterManagerList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleClusterMasters(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApiV3.ClusterMasterList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	retCRs := make([]splcommon.MetaObject, 0)
	for i := range list.Items {
		cr := &list.Items[i]
		if cr.Status.TelAppInstalled {
			retCRs = append(retCRs, cr)
		}
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, retCRs, nil
}

func handleMonitoringConsoles(ctx context.Context, client splcommon.ControllerClient) (interface{}, []splcommon.MetaObject, error) {
	var list enterpriseApi.MonitoringConsoleList
	err := client.List(ctx, &list)
	if err != nil {
		return nil, nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil, nil
	}

	retData := make(map[string]interface{})
	for i := range list.Items {
		cr := &list.Items[i]
		retData[cr.GetName()] = collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources)
	}
	return retData, nil, nil
}

func CollectCMTelData(ctx context.Context, cm *corev1.ConfigMap, data map[string]interface{}) {
	logger := logging.FromContext(ctx).With("func", "collectCMTelData")
	logger.InfoContext(ctx, "Start")

	for key, val := range cm.Data {
		if key == telStatusKey {
			continue
		}
		var compData interface{}
		logger.InfoContext(ctx, "Processing telemetry input from other components", "key", key)
		err := json.Unmarshal([]byte(val), &compData)
		if err != nil {
			logger.InfoContext(ctx, "Not able to unmarshal. Will include the input as string", "key", key, "value", val)
			data[key] = val
		} else {
			data[key] = compData
			logger.InfoContext(ctx, "Got telemetry input", "key", key, "value", val)
		}
	}
}

func getCurrentStatus(ctx context.Context, cm *corev1.ConfigMap) *TelemetryStatus {
	logger := logging.FromContext(ctx).With("func", "getCurrentStatus")

	defaultStatus := &TelemetryStatus{
		LastTransmission: "",
		Test:             defaultTestMode,
		SokVersion:       defaultTestVersion,
	}
	if val, ok := cm.Data[telStatusKey]; ok {
		var status TelemetryStatus
		err := json.Unmarshal([]byte(val), &status)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to unmarshal telemetry status", "error", err, "value", val)
			return defaultStatus
		} else {
			logger.InfoContext(ctx, "Got current telemetry status from configmap", "status", status)
			return &status
		}
	}

	logger.InfoContext(ctx, "No status set in configmap")
	return defaultStatus
}

func SendTelemetry(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, data map[string]interface{}, test bool) bool {
	logger := logging.FromContext(ctx).With("func", "sendTelemetry",
		"name", cr.GetObjectMeta().GetName(),
		"namespace", cr.GetObjectMeta().GetNamespace(),
		"kind", cr.GetObjectKind().GroupVersionKind().Kind)
	logger.InfoContext(ctx, "Start")

	var instanceID InstanceType
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		instanceID = SplunkStandalone
	case "LicenseManager":
		instanceID = SplunkLicenseManager
	case "LicenseMaster":
		instanceID = SplunkLicenseMaster
	case "SearchHeadCluster":
		instanceID = SplunkSearchHead
	case "ClusterMaster":
		instanceID = SplunkClusterMaster
	case "ClusterManager":
		instanceID = SplunkClusterManager
	default:
		logger.ErrorContext(ctx, "Failed to determine instance type for telemetry", "error", fmt.Errorf("unknown CR kind"))
		return false
	}

	serviceName := GetSplunkServiceName(instanceID, cr.GetName(), false)
	serviceFQDN := splcommon.GetServiceFQDN(cr.GetNamespace(), serviceName)
	logger.InfoContext(ctx, "Got service FQDN", "serviceFQDN", serviceFQDN)

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), defaultSecretObjName)
	if err != nil {
		logger.ErrorContext(ctx, "Could not access default secret object", "error", err)
		return false
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		logger.InfoContext(ctx, "Failed to find admin password")
		return false
	}
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", serviceFQDN), "admin", string(adminPwd))

	var licenseInfo map[string]splclient.LicenseInfo
	licenseInfo, err = splunkClient.GetLicenseInfo()
	if err != nil {
		logger.ErrorContext(ctx, "Failed to retrieve the license info", "error", err)
		return false
	} else {
		data[telLicenseInfoKey] = licenseInfo
	}
	telemetry := Telemetry{
		Type:          "event",
		Component:     "sok",
		OptInRequired: 2,
		Data:          data,
		Test:          test,
	}

	path := fmt.Sprintf("/servicesNS/nobody/%s/telemetry-metric", telAppNameStr)
	bodyBytes, err := json.Marshal(telemetry)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal to bytes", "error", err)
		return false
	}
	logger.InfoContext(ctx, "Sending request", "path", path)

	response, err := splunkClient.SendTelemetry(path, bodyBytes)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to send telemetry", "error", err)
		return false
	}

	logger.InfoContext(ctx, "Successfully sent telemetry", "response", response)
	return true
}
