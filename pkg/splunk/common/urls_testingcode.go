package common

import (
	"bytes"
	_ "embed"
	"encoding/json"
)

// Endpoints
// List of all endpoints (Full URls) used by the Testing code

// ***** Cluster Manager *****

// Test URLs for Cluster Manager based on Manager-service
const (

	//TestServiceURLClusterManagerClusterConfig = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/config?count=0&output_mode=json"
	TestServiceURLClusterManagerClusterConfig = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089" + URIClusterManagerClusterConfig + "?count=0&output_mode=json"

	//TestServiceURLClusterManagerGetInfo = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/info?count=0&output_mode=json"
	TestServiceURLClusterManagerGetInfo = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089" + URIClusterManagerGetInfo + "?count=0&output_mode=json"

	//TestServiceURLClusterManagerGetPeers = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/peers?count=0&output_mode=json"
	TestServiceURLClusterManagerGetPeers = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089" + URIClusterManagerGetPeers + "?count=0&output_mode=json"

	//TestServiceURLClusterManagerRemovePeers = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/control/control/remove_peers"
	TestServiceURLClusterManagerRemovePeers = "https://splunk-master1-cluster-master-service.test.svc.cluster.local:8089" + URIClusterManagerRemovePeers

	//TestServiceURLClusterManagerMgmtPort = "https://splunk--cluster-master-service.test.svc.cluster.local:8089"
	TestServiceURLClusterManagerMgmtPort = "https://splunk--cluster-master-service.test.svc.cluster.local:8089"
)

// Test K8s components for Cluster Manager based on Stack1
const (

	//TestStack1ClusterManager = "splunk-stack1-cluster-master"
	TestStack1ClusterManager = "splunk-stack1-" + ClusterManager

	//TestStack1ClusterManagerID = "splunk-stack1-cluster-master-0"
	TestStack1ClusterManagerID = TestStack1ClusterManager + "-%s"

	//TestStack1ClusterManagerService = "Service-test-splunk-stack1-cluster-master-service"
	TestStack1ClusterManagerService = "Service-test-splunk-stack1-" + TestClusterManagerService

	//TestStack1ClusterManagerSecret = "Secret-test-splunk-stack1-cluster-master-secret-v1"
	TestStack1ClusterManagerSecret = "Secret-test-splunk-stack1-" + TestClusterManagerSecret + "-%s"

	//TestStack1ClusterManagerPod = "Pod-test-splunk-stack1-cluster-master-0"
	TestStack1ClusterManagerPod = "Pod-test-splunk-stack1-" + ClusterManager + "-%s"

	//TestStack1ClusterManagerStatefulSet = "StatefulSet-test-splunk-stack1-cluster-master"
	TestStack1ClusterManagerStatefulSet = "StatefulSet-test-splunk-stack1-" + ClusterManager

	//TestStack1ClusterManagerSmartStore = "splunk-stack1-clustermaster-smartstore"
	TestStack1ClusterManagerSmartStore = "splunk-stack1-clustermaster-smartstore"

	//TestStack1ClusterManagerConfigMapSmartStore = "ConfigMap-test-splunk-stack1-clustermaster-smartstore"
	TestStack1ClusterManagerConfigMapSmartStore = "ConfigMap-test-splunk-stack1-clustermaster-smartstore"

	//TestStack1ClusterManagerConfigMapAppList = "ConfigMap-test-splunk-stack1-clustermaster-app-list"
	TestStack1ClusterManagerConfigMapAppList = "ConfigMap-test-splunk-stack1-clustermaster-app-list"
)

// Test K8s components for Cluster Manager Single Exceptions
const (

	//TestClusterManagerService = "cluster-master-service"
	TestClusterManagerService = ClusterManager + "-service"

	//TestClusterManagerSecret = "cluster-master-secret"
	TestClusterManagerSecret = ClusterManager + "-secret"

	//TestClusterManagerPod = "Pod-test-splunk-master1-cluster-master-0"
	TestClusterManagerPod = "Pod-test-splunk-master1-" + ClusterManager + "-%s"

	//TestExampleClusterManagerMgmtPort = "splunk-example-cluster-master-service:8089"
	TestExampleClusterManagerMgmtPort = "splunk-example-" + TestClusterManagerService + ":8089"

	//TestClusterManager = "splunk-%s-cluster-master"
	TestClusterManager = "splunk-%s-cluster-master"

	//TestClusterManagerID = "splunk-%s-cluster-master-%s"
	TestClusterManagerID = "splunk-%s-cluster-master-%s"

	//TestClusterManagerDashed = "-cluster-master-"
	TestClusterManagerDashed = "-" + ClusterManager + "-"

	//TestClusterManager1 = "ClusterMaster-test-master1"
	TestClusterManager1 = "ClusterMaster-test-master1"

	//TestClusterManager1Secrets = "splunk-master1-indexer-secrets"
	TestClusterManager1Secrets = "splunk-master1-indexer-secrets"
)

// Test URLs for Cluster Peers based on Stack1
const (

	//TestURLPeerHeadlessDecommission = "https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089/services/cluster/slave/control/control/decommission"
	TestURLPeerHeadlessDecommission = "https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089" + URIPeerDecommission
)

// ***** License Manager *****

// Test K8s components for License Manager based on Stack1
const (

	//TestStack1LicenseManager = "test-splunk-stack1-license-master"
	TestStack1LicenseManager = "test-splunk-stack1-" + LicenseManager

	//TestStack1LicenseManagerService = "splunk-stack1-license-master-service"
	TestStack1LicenseManagerService = "splunk-stack1-license-master-service"

	//TestStack1LicenseManagerServiceTest = "Service-test-splunk-stack1-licese-master"
	TestStack1LicenseManagerServiceTest = "Service-test-splunk-stack1-" + LicenseManager

	//TestStack1LicenseManagerServiceTestService = "Service-test-splunk-stack1-license-master-service"
	TestStack1LicenseManagerServiceTestService = TestStack1LicenseManagerServiceTest + "-service"

	//TestStack1LicenseManagerSecret = "Secret-test-splunk-stack1-license-master-secret-v1"
	TestStack1LicenseManagerSecret = "Secret-" + TestStack1LicenseManager + "-secret-v1"

	//TestStack1LicenseManagerConfigMapAppList = "ConfigMap-test-splunk-stack1-licensemaster-app-list"
	TestStack1LicenseManagerConfigMapAppList = "ConfigMap-test-splunk-stack1-licensemaster-app-list"

	//TestStack1LicenseManagerStatefulSet = "StatefulSet-test-splunk-stack1-license-master"
	TestStack1LicenseManagerStatefulSet = "StatefulSet-" + TestStack1LicenseManager

	//TestStack1LicenseManagerClusterLocal = "splunk-stack1-license-master-service.test.svc.cluster.local"
	TestStack1LicenseManagerClusterLocal = TestStack1LicenseManagerService + ".test.svc.cluster.local"
)

// Test K8s components for License Manager Single Exceptions
const (
	//TestT3LicenseManagerService = "splunk-t3-license-master-service"
	TestT3LicenseManagerService = "splunk-t3-license-master-service"

	//TestLicenseManagerMgmtPort = "license-master-service:8089"
	TestLicenseManagerMgmtPort = LicenseManager + "-service:8089"
)

// ***** Deployer *****

// Single Exceptions
const (

	//TestDeployerDashed = "-deployer-"
	TestDeployerDashed = "-deployer-"
)

// ***** Body Responses *****

// JSON body responses loaded from fixture files
//
//nolint:gochecknoinits
func init() {
	compact := func(s *string) {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(*s)); err == nil {
			*s = buf.String()
		}
	}
	compact(&TestGetMonitoringConsoleStatefulSet)
	compact(&TestGetSearchHeadStatefulSet)
	compact(&TestGetSearchHeadStatefulSetT2)
	compact(&TestGetSearchHeadStatefulSetT3)
	compact(&TestGetSearchHeadStatefulSetT4)
	compact(&TestGetSearchHeadStatefulSetApps)
	compact(&TestGetSearchHeadStatefulSetNewPort)
	compact(&TestGetCMStatefulSet)
	compact(&TestGetCMStatefulSetLicense)
	compact(&TestGetCMStatefulSetURL)
	compact(&TestGetCMStatefulSetApps)
	compact(&TestGetCMStatefulSetServiceAccount)
	compact(&TestGetCMStatefulSetExtraEnv)
	compact(&TestGetCMInfo)
	compact(&TestGetCMInfoEmpty)
	compact(&TestGetCMPeers)
	compact(&TestGetIndexerClusterPeerInfo)
	compact(&TestGetIndexerClusterPeerInfoEmpty)
	compact(&TestUpdateStatusInvalidResponse0)
	compact(&TestUpdateStatusInvalidResponse1)
	compact(&TestInvalidPeerStatusInScaleDownInfo)
	compact(&TestInvalidPeerStatusInScaleDownPeer)
	compact(&TestInvalidPeerInFinishRecycleInfo)
	compact(&TestInvalidPeerInFinishRecyclePeer)
	compact(&TestIndexerClusterPodManagerInfo)
	compact(&TestIndexerClusterPodManagerPeer)
	compact(&TestGetIndexerStatefulSettest0)
	compact(&TestGetIndexerStatefulSettest1)
	compact(&TestGetIndexerStatefulSettest2)
	compact(&TestGetIndexerStatefulSettest3)
	compact(&TestGetIndexerStatefulSettest4)
	compact(&TestGetIndexerStatefulSettest5)
	compact(&TestMCApplyChanges)
	compact(&TestGetMCAssetTable)
	compact(&TestGetClusterInfo)
	compact(&TestGetLMStatefulSetT1)
	compact(&TestGetLMStatefulSetT2)
	compact(&TestGetLMStatefulSetT3)
	compact(&TestGetLMStatefulSetT4)
	compact(&TestGetLMStatefulSetT5)
	compact(&TestVerifyRFPeers)
	compact(&TestVerifyRFPeersMultiSite)
	compact(&TestGetStandaloneStatefulSetT1)
	compact(&TestGetStandaloneStatefulSetT2)
	compact(&TestGetStandaloneStatefulSetT3)
	compact(&TestGetStandaloneStatefulSetT4)
	compact(&TestApplyAppListingConfigMap)
	compact(&TestGetMonitoringConsoleStatefulSetT1)
	compact(&TestGetMonitoringConsoleStatefulSetT2)
	compact(&TestGetMonitoringConsoleStatefulSetT3)
	compact(&TestGetMonitoringConsoleStatefulSetT4)
	compact(&TestGetMonitoringConsoleStatefulSetT5)
	compact(&TestGetMonitoringConsoleStatefulSetT6)
}

var (
	//go:embed testdata/fixtures/get_monitoring_console_stateful_set.json
	TestGetMonitoringConsoleStatefulSet string

	//go:embed testdata/fixtures/get_search_head_stateful_set.json
	TestGetSearchHeadStatefulSet string

	//go:embed testdata/fixtures/get_search_head_stateful_set_t2.json
	TestGetSearchHeadStatefulSetT2 string

	//go:embed testdata/fixtures/get_search_head_stateful_set_t3.json
	TestGetSearchHeadStatefulSetT3 string

	//go:embed testdata/fixtures/get_search_head_stateful_set_t4.json
	TestGetSearchHeadStatefulSetT4 string

	//go:embed testdata/fixtures/get_search_head_stateful_set_apps.json
	TestGetSearchHeadStatefulSetApps string

	//go:embed testdata/fixtures/get_search_head_stateful_set_new_port.json
	TestGetSearchHeadStatefulSetNewPort string

	//go:embed testdata/fixtures/get_cm_stateful_set.json
	TestGetCMStatefulSet string

	//go:embed testdata/fixtures/get_cm_stateful_set_license.json
	TestGetCMStatefulSetLicense string

	//go:embed testdata/fixtures/get_cm_stateful_set_url.json
	TestGetCMStatefulSetURL string

	//go:embed testdata/fixtures/get_cm_stateful_set_apps.json
	TestGetCMStatefulSetApps string

	//go:embed testdata/fixtures/get_cm_stateful_set_service_account.json
	TestGetCMStatefulSetServiceAccount string

	//go:embed testdata/fixtures/get_cm_stateful_set_extra_env.json
	TestGetCMStatefulSetExtraEnv string

	//go:embed testdata/fixtures/get_cm_info.json
	TestGetCMInfo string

	//go:embed testdata/fixtures/get_cm_info_empty.json
	TestGetCMInfoEmpty string

	//go:embed testdata/fixtures/get_cm_peers.json
	TestGetCMPeers string

	//go:embed testdata/fixtures/get_indexer_cluster_peer_info.json
	TestGetIndexerClusterPeerInfo string

	//go:embed testdata/fixtures/get_indexer_cluster_peer_info_empty.json
	TestGetIndexerClusterPeerInfoEmpty string

	//go:embed testdata/fixtures/update_status_invalid_response0.json
	TestUpdateStatusInvalidResponse0 string

	//go:embed testdata/fixtures/update_status_invalid_response1.json
	TestUpdateStatusInvalidResponse1 string

	//go:embed testdata/fixtures/invalid_peer_status_in_scale_down_info.json
	TestInvalidPeerStatusInScaleDownInfo string

	//go:embed testdata/fixtures/invalid_peer_status_in_scale_down_peer.json
	TestInvalidPeerStatusInScaleDownPeer string

	//go:embed testdata/fixtures/invalid_peer_in_finish_recycle_info.json
	TestInvalidPeerInFinishRecycleInfo string

	//go:embed testdata/fixtures/invalid_peer_in_finish_recycle_peer.json
	TestInvalidPeerInFinishRecyclePeer string

	//go:embed testdata/fixtures/indexer_cluster_pod_manager_info.json
	TestIndexerClusterPodManagerInfo string

	//go:embed testdata/fixtures/indexer_cluster_pod_manager_peer.json
	TestIndexerClusterPodManagerPeer string

	//go:embed testdata/fixtures/get_indexer_stateful_settest0.json
	TestGetIndexerStatefulSettest0 string

	//go:embed testdata/fixtures/get_indexer_stateful_settest1.json
	TestGetIndexerStatefulSettest1 string

	//go:embed testdata/fixtures/get_indexer_stateful_settest2.json
	TestGetIndexerStatefulSettest2 string

	//go:embed testdata/fixtures/get_indexer_stateful_settest3.json
	TestGetIndexerStatefulSettest3 string

	//go:embed testdata/fixtures/get_indexer_stateful_settest4.json
	TestGetIndexerStatefulSettest4 string

	//go:embed testdata/fixtures/get_indexer_stateful_settest5.json
	TestGetIndexerStatefulSettest5 string

	//go:embed testdata/fixtures/mc_apply_changes.json
	TestMCApplyChanges string

	//go:embed testdata/fixtures/get_mc_asset_table.json
	TestGetMCAssetTable string

	//go:embed testdata/fixtures/get_cluster_info.json
	TestGetClusterInfo string

	//go:embed testdata/fixtures/get_lm_stateful_set_t1.json
	TestGetLMStatefulSetT1 string

	//go:embed testdata/fixtures/get_lm_stateful_set_t2.json
	TestGetLMStatefulSetT2 string

	//go:embed testdata/fixtures/get_lm_stateful_set_t3.json
	TestGetLMStatefulSetT3 string

	//go:embed testdata/fixtures/get_lm_stateful_set_t4.json
	TestGetLMStatefulSetT4 string

	//go:embed testdata/fixtures/get_lm_stateful_set_t5.json
	TestGetLMStatefulSetT5 string

	//go:embed testdata/fixtures/verify_rf_peers.json
	TestVerifyRFPeers string

	//go:embed testdata/fixtures/verify_rf_peers_multi_site.json
	TestVerifyRFPeersMultiSite string

	//go:embed testdata/fixtures/get_standalone_stateful_set_t1.json
	TestGetStandaloneStatefulSetT1 string

	//go:embed testdata/fixtures/get_standalone_stateful_set_t2.json
	TestGetStandaloneStatefulSetT2 string

	//go:embed testdata/fixtures/get_standalone_stateful_set_t3.json
	TestGetStandaloneStatefulSetT3 string

	//go:embed testdata/fixtures/get_standalone_stateful_set_t4.json
	TestGetStandaloneStatefulSetT4 string

	//go:embed testdata/fixtures/apply_app_listing_config_map.json
	TestApplyAppListingConfigMap string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t1.json
	TestGetMonitoringConsoleStatefulSetT1 string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t2.json
	TestGetMonitoringConsoleStatefulSetT2 string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t3.json
	TestGetMonitoringConsoleStatefulSetT3 string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t4.json
	TestGetMonitoringConsoleStatefulSetT4 string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t5.json
	TestGetMonitoringConsoleStatefulSetT5 string

	//go:embed testdata/fixtures/get_monitoring_console_stateful_set_t6.json
	TestGetMonitoringConsoleStatefulSetT6 string
)
