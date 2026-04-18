package common

// List of all Paths used in the Splunk Operator

// List of Splunk Enterprise Paths
const (

	//PeerAppsLoc
	PeerAppsLoc = "etc/peer-apps"

	//ManagerAppsLoc
	ManagerAppsLoc = "etc/manager-apps"

	//SHCluster
	SHCluster = "etc/shcluster"

	//SHClusterAppsLoc = "etc/shcluster/apps"
	SHClusterAppsLoc = SHCluster + "/apps"
)

// List of Operator Paths
const (

	//ManagerAppsOperatorLocal
	OperatorClusterManagerAppsLocal = "/opt/splk/etc/manager-apps/splunk-operator/local"

	//OperatorClusterManagerAppsLocalIndexesConf
	OperatorClusterManagerAppsLocalIndexesConf = "/opt/splk/etc/manager-apps/splunk-operator/local/indexes.conf"

	//OperatorClusterManagerAppsLocalServerConf
	OperatorClusterManagerAppsLocalServerConf = "/opt/splk/etc/manager-apps/splunk-operator/local/server.conf"

	//OperatorMountLocalIndexesConf
	OperatorMountLocalIndexesConf = "/mnt/splunk-operator/local/indexes.conf"

	//OperatorMountLocalServerConf
	OperatorMountLocalServerConf = "/mnt/splunk-operator/local/server.conf"

	//OperatorClusterManagerAppsLocalOutputsConf
	OperatorClusterManagerAppsLocalOutputsConf = "/opt/splk/etc/manager-apps/splunk-operator/local/outputs.conf"

	//OperatorClusterManagerAppsLocalInputsConf
	OperatorClusterManagerAppsLocalInputsConf = "/opt/splk/etc/manager-apps/splunk-operator/local/inputs.conf"

	//OperatorClusterManagerAppsLocalDefaultModeConf
	OperatorClusterManagerAppsLocalDefaultModeConf = "/opt/splk/etc/manager-apps/splunk-operator/local/default-mode.conf"

	//OperatorMountLocalOutputsConf
	OperatorMountLocalOutputsConf = "/mnt/splunk-operator/local/outputs.conf"

	//OperatorMountLocalInputsConf
	OperatorMountLocalInputsConf = "/mnt/splunk-operator/local/inputs.conf"

	//OperatorMountLocalDefaultModeConf
	OperatorMountLocalDefaultModeConf = "/mnt/splunk-operator/local/default-mode.conf"
)
