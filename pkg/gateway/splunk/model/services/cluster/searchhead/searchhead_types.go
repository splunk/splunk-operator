package searchhead

// SearchHeadCaptainInfo represents the status of the search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Finfo
type SearchHeadCaptainInfo struct {
	// Id of this SH cluster. This is used as the unique identifier for the Search Head Cluster in bundle replication and acceleration summary management.
	Identifier string `json:"id"`

	// Time when the current captain was elected
	ElectedCaptain int64 `json:"elected_captain"`

	// Indicates if the searchhead cluster is initialized.
	Initialized bool `json:"initialized_flag"`

	// The name for the captain. Displayed on the Splunk Web manager page.
	Label string `json:"label"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenance_mode"`

	// Flag to indicate if more then replication_factor peers have joined the cluster.
	MinPeersJoined bool `json:"min_peers_joined_flag"`

	// URI of the current captain.
	PeerSchemeHostPort string `json:"peer_scheme_host_port"`

	// Indicates whether the captain is restarting the members in a searchhead cluster.
	RollingRestart bool `json:"rolling_restart_flag"`

	// Indicates whether the captain is ready to begin servicing, based on whether it is initialized.
	ServiceReady bool `json:"service_ready_flag"`

	// Timestamp corresponding to the creation of the captain.
	StartTime int64 `json:"start_time"`
}
