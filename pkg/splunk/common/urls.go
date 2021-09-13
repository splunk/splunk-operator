package common

// URLs for Splunk Endpoints
// List of most common endpoints used in the Splunk Operator repo

const (

	//ManagerInfoAPI = "/services/cluster/master/info"
	ManagerInfoAPI = Services + Manager + Info

	//ManagerPeersAPI = "/services/cluster/master/peers"
	ManagerPeersAPI = Services + Manager + Peers

	//ClusterServiceConfig = "/services/cluster/config?"
	ClusterServiceConfig = Services + Cluster + Config + QM

	//PeerInfoAPI = "/services/cluster/slave/info"
	PeerInfoAPI = Services + Peer + Info

	//RemovePeersAPI = "/services/cluster/master/control/control/remove_peers?"
	RemovePeersAPI = Services + Manager + Control + Control + RemovePeers + QM

	//DecommissionAPI = "/services/cluster/slave/control/control/decommission?"
	DecommissionAPI = Services + Peer + Control + Control + Decommission + QM

	//ApplyBundleAPI = "/services/cluster/master/control/default/apply"
	ApplyBundleAPI = Services + Manager + Control + Default + Apply

	//SearchHeadsAPI = "/services/cluster/master/searchheads?"
	SearchHeadsAPI = Services + Manager + SearchHeads + QM

	//ManagerHealthAPI = "/services/cluster/master/health?"
	ManagerHealthAPI = Services + Manager + Health + QM

	//ManagerSitesAPI = "/services/cluster/master/sites?output_mode=json"
	ManagerSitesAPI = Services + Manager + Sites + QM
)

// Base endpoints
const (

	//*****************
	//   Base Hosts
	//*****************

	//Port8089 = ":8089"
	Port8089 = ":8089"

	//LocalURL = "https://localhost:8089"
	LocalURL = "https://localhost" + Port8089

	//Cluster = "/cluster"
	Cluster = "/cluster"

	//Manager = "/cluster/master"
	Manager = Cluster + "/master"

	//Peer = "/cluster/slave"
	Peer = Cluster + "/slave"

	//Spl = "splunk"
	Spl = "splunk"

	//*****************
	//  Base Outputs
	//*****************

	//JSONOutput =  "output_mode=json"
	JSONOutput = "output_mode=json"

	//CountZero = "count=0"
	CountZero = "count=0"

	//*****************
	//   Base APIs
	//*****************

	//Services = "/services"
	Services = "/services"

	//Info = "/info"
	Info = "/info"

	//Peers = "/peers"
	Peers = "/peers"

	//Data = "/data"
	Data = "/data"

	//Sites = "/sites"
	Sites = "/sites"

	//Health = "/health"
	Health = "/health"

	//SearchHeads = "/searchheads"
	SearchHeads = "/searchheads"

	//Indexes = "/indexes"
	Indexes = "/indexes"

	//Control = "/control"
	Control = "/control"

	//RemovePeers = "/remove_peers"
	RemovePeers = "/remove_peers"

	//Decommission = "/decommission"
	Decommission = "/decommission"

	//Default = "/default"
	Default = "/default"

	//Apply = "/apply"
	Apply = "/apply"

	//Config = "/config"
	Config = "/config"

	//LocalsPeer = "/localslave"
	LocalsPeer = "/localslave"

	//Licenser = "/licenser"
	Licenser = "/licenser"

	//QM = Question Mark
	QM = "?"

	//Amp = ampersand
	Amp = "&"

	//Dash = "-"
	Dash = "-"
)
