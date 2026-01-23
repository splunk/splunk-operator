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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	requeAfterInSeconds = 30
)

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

type Telemetry struct {
	Type          string                 `json:"type"`
	Component     string                 `json:"component"`
	OptInRequired int                    `json:"optInRequired"`
	Data          map[string]interface{} `json:"data"`
	Test          bool                   `json:"test"`
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

	// Add SOK version
	data[telSOKVersionKey] = "3.0.0"
	// Add per CR telemetry
	crList := getAllCustomResources(ctx, client)

	collectCRTelData(ctx, client, crList, data)
	// Add telemetry set in this configmap, i.e splunk POD's telemetry
	CollectCMTelData(ctx, cm, data)

	// Now send the telemetry
	for _, crs := range crList {
		for _, cr := range crs {
			success := SendTelemetry(ctx, client, cr, data)
			if success {
				return result, nil
			}
		}
	}

	return result, errors.New("Failed to send telemetry data")
}

func getAllCustomResources(ctx context.Context, client splcommon.ControllerClient) map[string][]splcommon.MetaObject {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("collectCRTelData")

	var crList map[string][]splcommon.MetaObject
	crList = make(map[string][]splcommon.MetaObject)

	//var instanceID InstanceType
	//var telAppName string

	var err error
	var standaloneList enterpriseApi.StandaloneList
	//instanceID = SplunkStandalone
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "stdaln")
	err = client.List(ctx, &standaloneList)
	if err != nil {
		scopedLog.Error(err, "Failed to list standalone objects")
	} else if len(standaloneList.Items) > 0 {
		crList[standaloneList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range standaloneList.Items {
			crList[standaloneList.Items[0].Kind] = append(crList[standaloneList.Items[0].Kind], &cr)
		}
	}

	var lmanagerList enterpriseApi.LicenseManagerList
	//instanceID = SplunkLicenseManager
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "lmanager")
	err = client.List(ctx, &lmanagerList)
	if err != nil {
		scopedLog.Error(err, "Failed to list LicenseManager objects")
	} else if len(lmanagerList.Items) > 0 {
		crList[lmanagerList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range lmanagerList.Items {
			crList[lmanagerList.Items[0].Kind] = append(crList[lmanagerList.Items[0].Kind], &cr)
		}
	}

	var lmasterList enterpriseApiV3.LicenseMasterList
	//instanceID = SplunkLicenseMaster
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "lmaster")
	err = client.List(ctx, &lmasterList)
	if err != nil {
		scopedLog.Error(err, "Failed to list LicenseMaster objects")
	} else if len(lmasterList.Items) > 0 {
		crList[lmasterList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range lmasterList.Items {
			crList[lmasterList.Items[0].Kind] = append(crList[lmasterList.Items[0].Kind], &cr)
		}
	}

	var shcList enterpriseApi.SearchHeadClusterList
	//instanceID = SplunkSearchHead
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "shc")
	err = client.List(ctx, &shcList)
	if err != nil {
		scopedLog.Error(err, "Failed to list SearchHeadCluster objects")
	} else if len(shcList.Items) > 0 {
		crList[shcList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range shcList.Items {
			crList[shcList.Items[0].Kind] = append(crList[shcList.Items[0].Kind], &cr)
		}
	}

	var idxList enterpriseApi.IndexerClusterList
	//instanceID = SplunkSearchHead
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "shc")
	err = client.List(ctx, &idxList)
	if err != nil {
		scopedLog.Error(err, "Failed to list SearchHeadCluster objects")
	} else if len(idxList.Items) > 0 {
		crList[idxList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range idxList.Items {
			crList[idxList.Items[0].Kind] = append(crList[idxList.Items[0].Kind], &cr)
		}
	}

	var cmanagerList enterpriseApi.ClusterManagerList
	//instanceID = SplunkClusterManager
	//telAppName = fmt.Sprintf(telAppNameTemplateStr, "cmanager")
	err = client.List(ctx, &cmanagerList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterManager objects")
	} else if len(cmanagerList.Items) > 0 {
		crList[cmanagerList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range cmanagerList.Items {
			crList[cmanagerList.Items[0].Kind] = append(crList[cmanagerList.Items[0].Kind], &cr)
		}
	}

	var cmasterList enterpriseApiV3.ClusterMasterList
	err = client.List(ctx, &cmasterList)
	if err != nil {
		scopedLog.Error(err, "Failed to list ClusterMaster objects")
	} else if len(cmasterList.Items) > 0 {
		crList[cmasterList.Items[0].Kind] = make([]splcommon.MetaObject, 0)
		for _, cr := range cmasterList.Items {
			crList[cmasterList.Items[0].Kind] = append(crList[cmasterList.Items[0].Kind], &cr)
		}
	}

	return crList
}

func getOwnedStatefulSets(
	ctx context.Context,
	c client.Client,
	cr client.Object,
) ([]appsv1.StatefulSet, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getOwnedStatefulSets")

	stsList := &appsv1.StatefulSetList{}
	if err := c.List(ctx, stsList,
		client.InNamespace(cr.GetNamespace()),
	); err != nil {
		scopedLog.Error(err, "Failed to list StatefulSets", "CR Name", cr.GetName())
		return nil, err
	}

	var result []appsv1.StatefulSet
	for _, sts := range stsList.Items {
		for _, owner := range sts.OwnerReferences {
			if owner.UID == cr.GetUID() {
				result = append(result, sts)
				break
			}
		}
	}
	return result, nil
}

func collectCRTelData(ctx context.Context, client splcommon.ControllerClient, crList map[string][]splcommon.MetaObject, data map[string]interface{}) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("collectCRTelData")
	scopedLog.Info("Start")

	for kind, crs := range crList {
		var perKindData map[string]interface{}
		perKindData = make(map[string]interface{})
		for _, cr := range crs {
			var perCRData []map[string]string
			perCRData = make([]map[string]string, 0)
			stsList, err := getOwnedStatefulSets(ctx, client, cr)
			if err != nil {
				scopedLog.Error(err, "Failed to get owned StatefulSets")
			} else if len(stsList) > 0 {
				for _, sts := range stsList {
					for _, container := range sts.Spec.Template.Spec.Containers {
						resPerContainer := map[string]string{
							"container_name": container.Name,
							"cpu_request":    container.Resources.Requests.Cpu().String(),
							"memory_request": container.Resources.Requests.Memory().String(),
							"cpu_limit":      container.Resources.Limits.Cpu().String(),
							"memory_limit":   container.Resources.Limits.Memory().String(),
						}
						perCRData = append(perCRData, resPerContainer)
					}
				}
			}
			perKindData[cr.GetName()] = perCRData
		}
		data[kind] = perKindData
	}
}

// CollectCMTelData is exported for testing
func CollectCMTelData(ctx context.Context, cm *corev1.ConfigMap, data map[string]interface{}) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("collectCMTelData")
	scopedLog.Info("Start")

	for key, val := range cm.Data {
		var compData interface{}
		scopedLog.Info("mqiu: Processing telemetry input from other components", "key", key, "value", val)
		err := json.Unmarshal([]byte(val), &compData)
		if err != nil {
			scopedLog.Info("Not able to unmarshal. Will include the input as string", "key", key, "value", val)
			data[key] = val
		} else {
			data[key] = compData
		}
	}
}

func isTest(ctx context.Context, client splcommon.ControllerClient, namespace string) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isTelemetryTest").WithValues(
		"namespace", namespace)
	scopedLog.Info("Start")

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      GetManagerConfigMapName("splunk-operator-"),
	}
	err := client.Get(ctx, key, cm)
	if err != nil {
		return isTestMode
	}
	if val, exists := cm.Data[testModeKey]; exists && val == "true" {
		scopedLog.Info("Test mode is enabled via configmap")
		return true
	}
	scopedLog.Info("Return test mode", "isTestMode", isTestMode)
	return isTestMode
}

// SendTelemetry is exported for testing
func SendTelemetry(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, data map[string]interface{}) bool {
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
	scopedLog.Info("mqiu: got service name", "serviceName", serviceName)

	splunkReadableData, err := splutil.GetSplunkReadableNamespaceScopedSecretData(ctx, client, cr.GetNamespace())
	if err != nil {
		scopedLog.Error(err, "Failed to retrieve secrets")
		return false
	}
	adminPwd, foundSecret := splunkReadableData["password"]
	if !foundSecret {
		scopedLog.Info("Failed to find admin password")
		return false
	}
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", serviceName), "admin", string(adminPwd))

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
		Test:          isTest(ctx, client, cr.GetNamespace()),
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
