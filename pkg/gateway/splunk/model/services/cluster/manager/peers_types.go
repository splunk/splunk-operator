package manager

import "time"

// Description: Access cluster manager peers.
// Rest End Point API: services/cluster/manager/peers
type LastDryRunBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}

type LastValidatedBundle struct {
	BundlePath    string `json:"bundle_path,omitempty"`
	Checksum      string `json:"checksum,omitempty"`
	IsValidBundle bool   `json:"is_valid_bundle,omitempty"`
	Timestamp     int    `json:"timestamp,omitempty"`
}

type LatestBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}

type PreviousActiveBundle struct {
	BundlePath string `json:"bundle_path,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Timestamp  int    `json:"timestamp,omitempty"`
}

type ClusterManagerPeerContent struct {
	ActiveBundleID    string `json:"active_bundle_id"`
	ApplyBundleStatus struct {
		InvalidBundle struct {
			BundleValidationErrors []interface{} `json:"bundle_validation_errors"`
			InvalidBundleID        string        `json:"invalid_bundle_id"`
		} `json:"invalid_bundle"`
		ReasonsForRestart             []interface{} `json:"reasons_for_restart"`
		RestartRequiredForApplyBundle bool          `json:"restart_required_for_apply_bundle"`
		Status                        string        `json:"status"`
	} `json:"apply_bundle_status"`
	BaseGenerationID        int `json:"base_generation_id"`
	BatchedReplicationCount int `json:"batched_replication_count"`
	BucketCount             int `json:"bucket_count"`
	BucketCountByIndex      struct {
		Audit     int `json:"_audit"`
		Internal  int `json:"_internal"`
		Telemetry int `json:"_telemetry"`
	} `json:"bucket_count_by_index"`
	BucketsRfByOriginSite struct {
		Default int `json:"default"`
		Site1   int `json:"site1"`
		Site2   int `json:"site2"`
	} `json:"buckets_rf_by_origin_site"`
	BucketsSfByOriginSite struct {
		Default int `json:"default"`
		Site1   int `json:"site1"`
		Site2   int `json:"site2"`
	} `json:"buckets_sf_by_origin_site"`
	EaiAcl                                 interface{}   `json:"eai:acl"`
	FixupSet                               []interface{} `json:"fixup_set"`
	HeartbeatStarted                       bool          `json:"heartbeat_started"`
	HostPortPair                           string        `json:"host_port_pair"`
	IndexingDiskSpace                      int64         `json:"indexing_disk_space"`
	IsSearchable                           bool          `json:"is_searchable"`
	IsValidBundle                          bool          `json:"is_valid_bundle"`
	Label                                  string        `json:"label"`
	LastDryRunBundle                       string        `json:"last_dry_run_bundle"`
	LastHeartbeat                          int           `json:"last_heartbeat"`
	LastValidatedBundle                    string        `json:"last_validated_bundle"`
	LatestBundleID                         string        `json:"latest_bundle_id"`
	MergingMode                            bool          `json:"merging_mode"`
	PeerRegisteredSummaries                bool          `json:"peer_registered_summaries"`
	PendingJobCount                        int           `json:"pending_job_count"`
	PrimaryCount                           int           `json:"primary_count"`
	PrimaryCountRemote                     int           `json:"primary_count_remote"`
	RegisterSearchAddress                  string        `json:"register_search_address"`
	ReplicationCount                       int           `json:"replication_count"`
	ReplicationPort                        int           `json:"replication_port"`
	ReplicationUseSsl                      bool          `json:"replication_use_ssl"`
	RestartRequiredForApplyingDryRunBundle bool          `json:"restart_required_for_applying_dry_run_bundle"`
	SearchStateCounter                     struct {
		PendingSearchable     int `json:"PendingSearchable"`
		PendingUnsearchable   int `json:"PendingUnsearchable"`
		Searchable            int `json:"Searchable"`
		SearchablePendingMask int `json:"SearchablePendingMask"`
		Unknown               int `json:"Unknown"`
		Unsearchable          int `json:"Unsearchable"`
	} `json:"search_state_counter"`
	Site          string `json:"site"`
	SplunkVersion string `json:"splunk_version"`
	Status        string `json:"status"`
	StatusCounter struct {
		Complete           int `json:"Complete"`
		NonStreamingTarget int `json:"NonStreamingTarget"`
		PendingDiscard     int `json:"PendingDiscard"`
		PendingTruncate    int `json:"PendingTruncate"`
		StreamingError     int `json:"StreamingError"`
		StreamingSource    int `json:"StreamingSource"`
		StreamingTarget    int `json:"StreamingTarget"`
		Unset              int `json:"Unset"`
	} `json:"status_counter"`
	SummaryReplicationCount int `json:"summary_replication_count"`
	TransientJobCount       int `json:"transient_job_count"`
}

type ClusterManagerPeerHeader struct {
	Links struct {
		Create string `json:"create"`
	} `json:"links"`
	Origin    string    `json:"origin"`
	Updated   time.Time `json:"updated"`
	Generator struct {
		Build   string `json:"build"`
		Version string `json:"version"`
	} `json:"generator"`
	Entry []struct {
		Name    string    `json:"name"`
		ID      string    `json:"id"`
		Updated time.Time `json:"updated"`
		Links   struct {
			Alternate string `json:"alternate"`
			List      string `json:"list"`
			Edit      string `json:"edit"`
		} `json:"links"`
		Author string `json:"author"`
		Acl    struct {
			App        string `json:"app"`
			CanList    bool   `json:"can_list"`
			CanWrite   bool   `json:"can_write"`
			Modifiable bool   `json:"modifiable"`
			Owner      string `json:"owner"`
			Perms      struct {
				Read  []string `json:"read"`
				Write []string `json:"write"`
			} `json:"perms"`
			Removable bool   `json:"removable"`
			Sharing   string `json:"sharing"`
		} `json:"acl"`
		Content ClusterManagerPeerContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
