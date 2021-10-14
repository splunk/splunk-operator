package common

// URLs for Splunk APIs and REST Endpoints

// ***** Cluster Manager *****

// List of URIs - Cluster Manager
const (

	//ClusterManager = "cluster-master"
	ClusterManager = "cluster-master"

	//URIClusterManagerClusterConfig = "/services/cluster/config?"
	URIClusterManagerClusterConfig = "/services/cluster/config?"

	//URICLusterManagerServices = "/services/cluster/master"
	URICLusterManagerServices = "/services/cluster/master"

	//URIClusterManagerGetInfo = "/services/cluster/master/info"
	URIClusterManagerGetInfo = URICLusterManagerServices + "/info"

	//URIClusterManagerGetPeers = "/services/cluster/master/peers"
	URIClusterManagerGetPeers = URICLusterManagerServices + "/peers"

	//URIClusterManagerRemovePeers = "/services/cluster/master/control/control/remove_peers?"
	URIClusterManagerRemovePeers = URICLusterManagerServices + "/control/control/remove_peers?"

	//URIClusterManagerApplyBundle = "/services/cluster/master/control/default/apply"
	URIClusterManagerApplyBundle = URICLusterManagerServices + "/control/default/apply"

	//URIClusterManagerGetHealth = "/services/cluster/master/health?"
	URIClusterManagerGetHealth = URICLusterManagerServices + "/health?"

	//URIClusterManagerGetSites = "/services/cluster/master/sites?"
	URIClusterManagerGetSites = URICLusterManagerServices + "/sites?"

	//URIClusterManagerGetSearchHeads = "/services/cluster/master/searchheads?"
	URIClusterManagerGetSearchHeads = URICLusterManagerServices + "/searchheads?"
)

// List of URLs - Cluster Manager
const (

	//LocalURLClusterManagerApplyBundle = "https://localhost:8089/services/cluster/master/control/default/apply"
	LocalURLClusterManagerApplyBundle = "https://localhost:8089" + URIClusterManagerApplyBundle

	//LocalURLClusterManagerGetInfo = "https://localhost:8089/services/cluster/master/info?count=0&output_mode=json"
	LocalURLClusterManagerGetInfo = "https://localhost:8089" + URIClusterManagerGetInfo + "?count=0&output_mode=json"

	//LocalURLClusterManagerGetInfoJSONOutput = "https://localhost:8089/services/cluster/master/info?output_mode=json"
	LocalURLClusterManagerGetInfoJSONOutput = "https://localhost:8089" + URIClusterManagerGetInfo + "output_mode=json"

	//LocalURLClusterManagerGetPeers = "https://localhost:8089/services/cluster/master/peers?count=0&output_mode=json"
	LocalURLClusterManagerGetPeers = "https://localhost:8089" + URIClusterManagerGetPeers + "?count=0&output_mode=json"

	//LocalURLClusterManagerGetPeersJSONOutput = "https://localhost:8089/services/cluster/master/peers?output_mode=json"
	LocalURLClusterManagerGetPeersJSONOutput = "https://localhost:8089" + URIClusterManagerGetPeers + "output_mode=json"

	//LocalURLClusterManagerRemovePeers = "https://localhost:8089/services/cluster/master/control/control/remove_peers?
	LocalURLClusterManagerRemovePeers = "https://localhost:8089" + URIClusterManagerRemovePeers

	//LocalURLClusterManagerGetSite = https://localhost:8089/services/cluster/master/sites?output_mode=json
	LocalURLClusterManagerGetSite = "https://localhost:8089" + URIClusterManagerGetSites + "output_mode=json"

	//LocalURLClusterManagerGetHealth = "https://localhost:8089/services/cluster/master/health?output_mode=json"
	LocalURLClusterManagerGetHealth = "https://localhost:8089" + URIClusterManagerGetHealth + "output_mode=json"

	//LocalURLClusterManagerGetSearchHeads = "https://localhost:8089/services/cluster/master/searchheads?output_mode=json"
	LocalURLClusterManagerGetSearchHeads = "https://localhost:8089" + URIClusterManagerGetSearchHeads + "output_mode=json"
)

// ***** Cluster Peers *****

// List of URIs - Cluster Peers
const (

	//URIPeerGetInfo = "/services/cluster/slave/info"
	URIPeerGetInfo = "/services/cluster/slave/info"

	//URIPeerDecommission = "/services/cluster/slave/control/control/decommission?"
	URIPeerDecommission = "/services/cluster/slave/control/control/decommission?"
)

// List of URLs - Cluster Peers
const (

	//URLPeerInfo = "https://localhost:8089/services/cluster/slave/info?count=0&output_mode=json"
	URLPeerInfo = "https://localhost:8089" + URIPeerGetInfo + "?count=0&output_mode=json"

	//URLPeerDecommission = "https://localhost:8089/services/cluster/slave/control/control/decommission?
	URLPeerDecommission = "https://localhost:8089" + URIPeerDecommission
)

// ***** License Manager *****

// List of URIs - License Manager
const (
	//LicenseManager = "license-master"
	LicenseManager = "license-master"
)

// List of URLs - License Manager/Peer
const (

	//LocalURLLicensePeerJSONOutput = "https://localhost:8089/services/licenser/localslave?output_mode=json"
	LocalURLLicensePeerJSONOutput = "https://localhost:8089/services/licenser/localslave?output_mode=json"
)
