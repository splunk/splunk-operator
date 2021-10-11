package common

// PATHS
// List of all Paths used in the Splunk Operator

const (

	//*****************
	// Splunk Enterprise Paths
	//*****************

	//PeerApps path
	PeerApps = "etc/slave-apps"

	//ManagerApps = "etc/master-apps"
	ManagerApps = "etc/master-apps"

	//SHCluster = "etc/shcluster/apps"
	SHCluster = "etc/shcluster"

	//SHClusterApps = "etc/shcluster/apps"
	SHClusterApps = SHCluster + "/apps"
)
