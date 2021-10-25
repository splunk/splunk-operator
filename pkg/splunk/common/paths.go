package common

// List of all Paths used in the Splunk Operator

//List of Splunk Enterprise Paths
const (

	//PeerAppsLoc
	PeerAppsLoc = "etc/slave-apps"

	//ManagerAppsLoc
	ManagerAppsLoc = "etc/master-apps"

	//SHCluster
	SHCluster = "etc/shcluster"

	//SHClusterAppsLoc = "etc/shcluster/apps"
	SHClusterAppsLoc = SHCluster + "/apps"
)

// List of Operator Paths
const (

	//ManagerAppsOperatorLocal
	OperatorClusterManagerAppsLocal = "/opt/splk/etc/master-apps/splunk-operator/local"

	//OperatorClusterManagerAppsLocalIndexesConf
	OperatorClusterManagerAppsLocalIndexesConf = "/opt/splk/etc/master-apps/splunk-operator/local/indexes.conf"

	//OperatorClusterManagerAppsLocalServerConf
	OperatorClusterManagerAppsLocalServerConf = "/opt/splk/etc/master-apps/splunk-operator/local/server.conf"

	//OperatorMountLocalIndexesConf
	OperatorMountLocalIndexesConf = "/mnt/splunk-operator/local/indexes.conf"

	//OperatorMountLocalServerConf
	OperatorMountLocalServerConf = "/mnt/splunk-operator/local/server.conf"
)
