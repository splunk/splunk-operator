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

// List of Operator Paths
const (

	//ManagerAppsOperatorLocal = "/opt/splk/etc/master-apps/splunk-operator/local"
	OperatorClusterManagerAppsLocal = "/opt/splk/etc/master-apps/splunk-operator/local"

	//OperatorClusterManagerAppsLocalIndexesConf = "/opt/splk/etc/master-apps/splunk-operator/local/indexes.conf"
	OperatorClusterManagerAppsLocalIndexesConf = "/opt/splk/etc/master-apps/splunk-operator/local/indexes.conf"

	//OperatorClusterManagerAppsLocalServerConf = "/opt/splk/etc/master-apps/splunk-operator/local/server.conf"
	OperatorClusterManagerAppsLocalServerConf = "/opt/splk/etc/master-apps/splunk-operator/local/server.conf"

	//OperatorMountLocalIndexesConf = "/mnt/splunk-operator/local/indexes.conf"
	OperatorMountLocalIndexesConf = "/mnt/splunk-operator/local/indexes.conf"

	//OperatorMountLocalServerConf = "/mnt/splunk-operator/local/server.conf"
	OperatorMountLocalServerConf = "/mnt/splunk-operator/local/server.conf"
)
