package manager

import "time"

// Description: Access information about cluster manager node.
// Rest End Point API: services/cluster/manager/info

type ClusterManagerInfoContent struct {
	ActiveBundle struct {
		BundlePath string `json:"bundle_path"`
		Checksum   string `json:"checksum"`
		Timestamp  int    `json:"timestamp"`
	} `json:"active_bundle"`
	ApplyBundleStatus struct {
		InvalidBundle struct {
			BundlePath                     string        `json:"bundle_path"`
			BundleValidationErrorsOnMaster []interface{} `json:"bundle_validation_errors_on_master"`
			Checksum                       string        `json:"checksum"`
			Timestamp                      int           `json:"timestamp"`
		} `json:"invalid_bundle"`
		ReloadBundleIssued bool   `json:"reload_bundle_issued"`
		Status             string `json:"status"`
	} `json:"apply_bundle_status"`
	AvailableSites               string      `json:"available_sites"`
	BackupAndRestorePrimaries    bool        `json:"backup_and_restore_primaries"`
	ControlledRollingRestartFlag bool        `json:"controlled_rolling_restart_flag"`
	EaiAcl                       interface{} `json:"eai:acl"`
	ForwarderSiteFailover        string      `json:"forwarder_site_failover"`
	IndexingReadyFlag            bool        `json:"indexing_ready_flag"`
	InitializedFlag              bool        `json:"initialized_flag"`
	Label                        string      `json:"label"`
	LastCheckRestartBundleResult bool        `json:"last_check_restart_bundle_result"`
	LastDryRunBundle             struct {
		BundlePath string `json:"bundle_path"`
		Checksum   string `json:"checksum"`
		Timestamp  int    `json:"timestamp"`
	} `json:"last_dry_run_bundle"`
	LastValidatedBundle struct {
		BundlePath    string `json:"bundle_path"`
		Checksum      string `json:"checksum"`
		IsValidBundle bool   `json:"is_valid_bundle"`
		Timestamp     int    `json:"timestamp"`
	} `json:"last_validated_bundle"`
	LatestBundle struct {
		BundlePath string `json:"bundle_path"`
		Checksum   string `json:"checksum"`
		Timestamp  int    `json:"timestamp"`
	} `json:"latest_bundle"`
	MaintenanceMode      bool `json:"maintenance_mode"`
	Multisite            bool `json:"multisite"`
	PreviousActiveBundle struct {
		BundlePath string `json:"bundle_path"`
		Checksum   string `json:"checksum"`
		Timestamp  int    `json:"timestamp"`
	} `json:"previous_active_bundle"`
	PrimariesBackupStatus   string `json:"primaries_backup_status"`
	QuietPeriodFlag         bool   `json:"quiet_period_flag"`
	RollingRestartFlag      bool   `json:"rolling_restart_flag"`
	RollingRestartOrUpgrade bool   `json:"rolling_restart_or_upgrade"`
	ServiceReadyFlag        bool   `json:"service_ready_flag"`
	SiteReplicationFactor   string `json:"site_replication_factor"`
	SiteSearchFactor        string `json:"site_search_factor"`
	StartTime               int    `json:"start_time"`
	SummaryReplication      string `json:"summary_replication"`
}

type ClusterManagerInfoHeader struct {
	Links struct {
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
		Content ClusterManagerInfoContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
