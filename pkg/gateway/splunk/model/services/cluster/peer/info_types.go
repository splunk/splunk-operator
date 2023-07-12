package peer

// Access cluster peer node information.
// endpoint : https://<host>:<mPort>/services/cluster/peer/info
type ActiveBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}
type LastDryRunBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}
type LatestBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}
type ClusterPeerInfo struct {
	ActiveBundle           ActiveBundle     `json:"active_bundle,omitempty"`
	BaseGenerationID       int              `json:"base_generation_id,omitempty"`
	EaiACL                 interface{}      `json:"eai:acl,omitempty"`
	IsRegistered           bool             `json:"is_registered,omitempty"`
	LastDryRunBundle       LastDryRunBundle `json:"last_dry_run_bundle,omitempty"`
	LastHeartbeatAttempt   int              `json:"last_heartbeat_attempt,omitempty"`
	LatestBundle           LatestBundle     `json:"latest_bundle,omitempty"`
	MaintenanceMode        bool             `json:"maintenance_mode,omitempty"`
	RegisteredSummaryState int              `json:"registered_summary_state,omitempty"`
	RestartState           string           `json:"restart_state,omitempty"`
	Site                   string           `json:"site,omitempty"`
	Status                 string           `json:"status,omitempty"`
}
