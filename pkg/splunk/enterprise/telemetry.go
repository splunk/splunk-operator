package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	requeAfterInSeconds = 86400 // Send telemetry once a day
	defaultTestMode     = "false"
	defaultTestVersion  = "unknown"

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

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyTelemetry")

	for k, v := range cm.Data {
		scopedLog.Info("Retrieved telemetry keys", "key", k, "value", v)
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
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateLastTransmissionTime")

	status.LastTransmission = time.Now().UTC().Format(time.RFC3339)
	updated, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		scopedLog.Error(err, "Failed to marshal telemetry status")
		return
	}
	cm.Data[telStatusKey] = string(updated)
	if err = client.Update(ctx, cm); err != nil {
		scopedLog.Error(err, "Failed to update telemetry status in configmap")
		return
	}
	scopedLog.Info("Updated last transmission time in configmap", "newStatus", cm.Data[telStatusKey])
}

func collectResourceTelData(resources corev1.ResourceRequirements, data map[string]string) {
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
		data[cpuRequestKey] = (&cpu).String()
		data[memoryRequestKey] = (&mem).String()
	} else {
		if cpuReq, ok := resources.Requests[corev1.ResourceCPU]; ok {
			data[cpuRequestKey] = cpuReq.String()
		} else {
			cpu := defaultResources.Requests[corev1.ResourceCPU]
			data[cpuRequestKey] = (&cpu).String()
		}
		if memReq, ok := resources.Requests[corev1.ResourceMemory]; ok {
			data[memoryRequestKey] = memReq.String()
		} else {
			mem := defaultResources.Requests[corev1.ResourceMemory]
			data[memoryRequestKey] = (&mem).String()
		}
	}

	if resources.Limits == nil {
		cpu := defaultResources.Limits[corev1.ResourceCPU]
		mem := defaultResources.Limits[corev1.ResourceMemory]
		data[cpuLimitKey] = (&cpu).String()
		data[memoryLimitKey] = (&mem).String()
	} else {
		if cpuLim, ok := resources.Limits[corev1.ResourceCPU]; ok {
			data[cpuLimitKey] = cpuLim.String()
		} else {
			cpu := defaultResources.Limits[corev1.ResourceCPU]
			data[cpuLimitKey] = (&cpu).String()
		}
		if memLim, ok := resources.Limits[corev1.ResourceMemory]; ok {
			data[memoryLimitKey] = memLim.String()
		} else {
			mem := defaultResources.Limits[corev1.ResourceMemory]
			data[memoryLimitKey] = (&mem).String()
		}
	}
}

func collectDeploymentTelData(ctx context.Context, client splcommon.ControllerClient, deploymentData map[string]interface{}) map[string][]splcommon.MetaObject {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("collectDeploymentTelData")

	var crWithTelAppList map[string][]splcommon.MetaObject
	crWithTelAppList = make(map[string][]splcommon.MetaObject)

	var err error
	var standaloneList enterpriseApi.StandaloneList
	err = client.List(ctx, &standaloneList)
	if err != nil {
		scopedLog.Error(err, "Failed to list standalone objects")
	} else if len(standaloneList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[standaloneList.Items[0].Kind] = perKindData
		for _, cr := range standaloneList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[standaloneList.Items[0].Kind] = append(crWithTelAppList[standaloneList.Items[0].Kind], &cr)
			}
		}
	}

	var lmanagerList enterpriseApi.LicenseManagerList
	err = client.List(ctx, &lmanagerList)
	if err != nil {
		scopedLog.Error(err, "Failed to list LicenseManager objects")
	} else if len(lmanagerList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[lmanagerList.Items[0].Kind] = perKindData
		for _, cr := range lmanagerList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[lmanagerList.Items[0].Kind] = append(crWithTelAppList[lmanagerList.Items[0].Kind], &cr)
			}
		}
	}

	var lmasterList enterpriseApiV3.LicenseMasterList
	err = client.List(ctx, &lmasterList)
	if err != nil {
		scopedLog.Error(err, "Failed to list LicenseMaster objects")
	} else if len(lmasterList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[lmasterList.Items[0].Kind] = perKindData
		for _, cr := range lmasterList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[lmasterList.Items[0].Kind] = append(crWithTelAppList[lmasterList.Items[0].Kind], &cr)
			}
		}
	}

	var shcList enterpriseApi.SearchHeadClusterList
	err = client.List(ctx, &shcList)
	if err != nil {
		scopedLog.Error(err, "Failed to list SearchHeadCluster objects")
	} else if len(shcList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[shcList.Items[0].Kind] = perKindData
		for _, cr := range shcList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[shcList.Items[0].Kind] = append(crWithTelAppList[shcList.Items[0].Kind], &cr)
			}
		}
	}

	var idxList enterpriseApi.IndexerClusterList
	err = client.List(ctx, &idxList)
	if err != nil {
		scopedLog.Error(err, "Failed to list IndexerCluster objects")
	} else if len(idxList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[idxList.Items[0].Kind] = perKindData
		for _, cr := range idxList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
		}
	}

	var cmanagerList enterpriseApi.ClusterManagerList
	err = client.List(ctx, &cmanagerList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterManager objects")
	} else if len(cmanagerList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[cmanagerList.Items[0].Kind] = perKindData
		for _, cr := range cmanagerList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[cmanagerList.Items[0].Kind] = append(crWithTelAppList[cmanagerList.Items[0].Kind], &cr)
			}
		}
	}

	var cmasterList enterpriseApiV3.ClusterMasterList
	err = client.List(ctx, &cmasterList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterMaster objects")
	} else if len(cmasterList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[cmasterList.Items[0].Kind] = perKindData
		for _, cr := range cmasterList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[cmasterList.Items[0].Kind] = append(crWithTelAppList[cmasterList.Items[0].Kind], &cr)
			}
		}
	}

	var licenseMasterList enterpriseApiV3.LicenseMasterList
	err = client.List(ctx, &licenseMasterList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterMaster objects")
	} else if len(licenseMasterList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[licenseMasterList.Items[0].Kind] = perKindData
		for _, cr := range licenseMasterList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[licenseMasterList.Items[0].Kind] = append(crWithTelAppList[licenseMasterList.Items[0].Kind], &cr)
			}
		}
	}

	var licenseManagerList enterpriseApi.LicenseManagerList
	err = client.List(ctx, &licenseManagerList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterMaster objects")
	} else if len(licenseManagerList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[licenseManagerList.Items[0].Kind] = perKindData
		for _, cr := range licenseManagerList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
			if cr.Status.TelAppInstalled {
				crWithTelAppList[licenseManagerList.Items[0].Kind] = append(crWithTelAppList[licenseManagerList.Items[0].Kind], &cr)
			}
		}
	}

	var mconsoleList enterpriseApi.MonitoringConsoleList
	err = client.List(ctx, &mconsoleList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterMaster objects")
	} else if len(mconsoleList.Items) > 0 {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		deploymentData[mconsoleList.Items[0].Kind] = perKindData
		for _, cr := range mconsoleList.Items {
			var crResourceData map[string]string
			crResourceData = make(map[string]string)
			perKindData[cr.GetName()] = crResourceData
			collectResourceTelData(cr.Spec.CommonSplunkSpec.Resources, crResourceData)
		}
	}

	return crWithTelAppList
}

func CollectCMTelData(ctx context.Context, cm *corev1.ConfigMap, data map[string]interface{}) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("collectCMTelData")
	scopedLog.Info("Start")

	for key, val := range cm.Data {
		if key == telStatusKey {
			continue
		}
		var compData interface{}
		scopedLog.Info("Processing telemetry input from other components", "key", key, "value", val)
		err := json.Unmarshal([]byte(val), &compData)
		if err != nil {
			scopedLog.Info("Not able to unmarshal. Will include the input as string", "key", key, "value", val)
			data[key] = val
		} else {
			data[key] = compData
		}
	}
}

func getCurrentStatus(ctx context.Context, cm *corev1.ConfigMap) *TelemetryStatus {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getCurrentStatus")

	defaultStatus := &TelemetryStatus{
		LastTransmission: "",
		Test:             defaultTestMode,
		SokVersion:       defaultTestVersion,
	}
	defaultStatus.LastTransmission = ""
	defaultStatus.Test = "true"
	if cm.Data != nil {
		if val, ok := cm.Data[telStatusKey]; ok {
			var status TelemetryStatus
			err := json.Unmarshal([]byte(val), &status)
			if err != nil {
				scopedLog.Error(err, "Failed to unmarshal telemetry status")
				return defaultStatus
			} else {
				return defaultStatus
			}
		}
	}

	scopedLog.Info("Failed")
	return defaultStatus
}

func SendTelemetry(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, data map[string]interface{}, test bool) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("sendTelemetry").WithValues(
		"name", cr.GetObjectMeta().GetName(),
		"namespace", cr.GetObjectMeta().GetNamespace(),
		"kind", cr.GetObjectKind().GroupVersionKind().Kind)
	scopedLog.Info("Start")

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
		return false
	}

	serviceName := GetSplunkServiceName(instanceID, cr.GetName(), false)
	serviceFQDN := splcommon.GetServiceFQDN(cr.GetNamespace(), serviceName)
	scopedLog.Info("Got service FQDN", "serviceFQDN", serviceFQDN)

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), cr.GetName(), defaultSecretObjName)
	if err != nil {
		scopedLog.Error(err, "Could not access default secret object")
		return false
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		scopedLog.Info("Failed to find admin password")
		return false
	}
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", serviceFQDN), "admin", string(adminPwd))

	var licenseInfo *splclient.LicenseInfo
	licenseInfo, err = splunkClient.GetLicenseInfo()
	if err != nil {
		scopedLog.Error(err, "Failed to retrieve the license info")
		return false
	} else {
		data[telLicenseInfoKey] = *licenseInfo
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
		scopedLog.Error(err, "Failed to marshal to bytes")
		return false
	}
	scopedLog.Info("Sending request", "path", path, "body", string(bodyBytes))

	response, err := splunkClient.SendTelemetry(path, bodyBytes)
	if err != nil {
		scopedLog.Error(err, "Failed to send telemetry")
		return false
	}

	scopedLog.Info("Successfully sent telemetry", "response", response)
	return true
}
