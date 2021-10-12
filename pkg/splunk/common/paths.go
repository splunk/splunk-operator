package common

// List of all Paths used in the Splunk Operator

//List of Splunk Enterprise Paths
const (

	//PeerAppsLoc = "etc/slave-apps"
	PeerAppsLoc = "etc/slave-apps"

	//ManagerAppsLoc = "etc/master-apps"
	ManagerAppsLoc = "etc/master-apps"

	//SHCluster = "etc/shcluster"
	SHCluster = "etc/shcluster"

	//SHClusterAppsLoc = "etc/shcluster/apps"
	SHClusterAppsLoc = SHCluster + "/apps"
)
