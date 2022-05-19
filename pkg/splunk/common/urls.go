package common

// URLs for Splunk APIs and REST Endpoints

// ***** Cluster Manager *****

// List of URIs - Cluster Manager
const (

	//ClusterManager = "cluster-master"
	ClusterManager = "cluster-master"

	//URIClusterManagerGetSearchHeads = "/services/cluster/master/searchheads"
	URIClusterManagerGetSearchHeads = "/services/cluster/master/searchheads"
)

// List of URLs - Cluster Manager
const (

	//ClusterManagerURL = "SPLUNK_CLUSTER_MASTER_URL"
	ClusterManagerURL = "SPLUNK_CLUSTER_MASTER_URL"

	//LocalURLClusterManagerGetSearchHeads = "https://localhost:8089/services/cluster/master/searchheads?output_mode=json"
	LocalURLClusterManagerGetSearchHeads = "https://localhost:8089" + URIClusterManagerGetSearchHeads + "?output_mode=json"
)

// ***** License Manager *****

// List of URIs - License Manager
const (
	//LicenseManager = "license-master"
	LicenseManager = "license-master"

	// LicenseManagerRole = "splunk_license_master"
	LicenseManagerRole = "splunk_license_master"

	// LicenseManagerURL = "SPLUNK_LICENSE_MASTER_URL"
	LicenseManagerURL = "SPLUNK_LICENSE_MASTER_URL"

	//LicenseManagerDMCGroup = "dmc_group_license_master"
	LicenseManagerDMCGroup = "dmc_group_license_master"
)

// List of URLs - License Manager/Peer
const (

	//LocalURLLicensePeerJSONOutput = "https://localhost:8089/services/licenser/localslave?output_mode=json"
	LocalURLLicensePeerJSONOutput = "https://localhost:8089/services/licenser/localslave?output_mode=json"

	//LocalURLLicenseManagerEdit = "https://localhost:8089/services/search/distributed/groups/dmc_group_license_master/edit"
	LocalURLLicenseManagerEdit = "https://localhost:8089/services/search/distributed/groups/dmc_group_license_master/edit"
)
