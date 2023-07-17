package manager

import "time"

// Description: Access cluster site information.
// Rest End Point: services/cluster/manager/sites
type ClusterManagerSiteContent struct {
	ActiveBundle struct {
		BundlePath string `json:"bundle_path,omitempty"`
		Checksum   string `json:"checksum,omitempty"`
		Timestamp  int    `json:"timestamp,omitempty"`
	} `json:"active_bundle,omitempty"`
	ApplyBundleStatus struct {
		InvalidBundle struct {
			BundlePath                     string        `json:"bundle_path,omitempty"`
			BundleValidationErrorsOnMaster []interface{} `json:"bundle_validation_errors_on_master,omitempty"`
			Checksum                       string        `json:"checksum,omitempty"`
			Timestamp                      int           `json:"timestamp,omitempty"`
		} `json:"invalid_bundle,omitempty"`
		ReloadBundleIssued bool   `json:"reload_bundle_issued,omitempty"`
		Status             string `json:"status,omitempty"`
	} `json:"apply_bundle_status,omitempty"`
	AvailableSites               string      `json:"available_sites,omitempty"`
	BackupAndRestorePrimaries    bool        `json:"backup_and_restore_primaries,omitempty"`
	ControlledRollingRestartFlag bool        `json:"controlled_rolling_restart_flag,omitempty"`
	EaiAcl                       interface{} `json:"eai:acl,omitempty"`
	ForwarderSiteFailover        string      `json:"forwarder_site_failover,omitempty"`
	IndexingReadyFlag            bool        `json:"indexing_ready_flag,omitempty"`
	InitializedFlag              bool        `json:"initialized_flag,omitempty"`
	Label                        string      `json:"label,omitempty"`
	LastCheckRestartBundleResult bool        `json:"last_check_restart_bundle_result,omitempty"`
	LastDryRunBundle             struct {
		BundlePath string `json:"bundle_path,omitempty"`
		Checksum   string `json:"checksum,omitempty"`
		Timestamp  int    `json:"timestamp,omitempty"`
	} `json:"last_dry_run_bundle,omitempty"`
	LastValidatedBundle struct {
		BundlePath    string `json:"bundle_path,omitempty"`
		Checksum      string `json:"checksum,omitempty"`
		IsValidBundle bool   `json:"is_valid_bundle,omitempty"`
		Timestamp     int    `json:"timestamp,omitempty"`
	} `json:"last_validated_bundle,omitempty"`
	LatestBundle struct {
		BundlePath string `json:"bundle_path,omitempty"`
		Checksum   string `json:"checksum,omitempty"`
		Timestamp  int    `json:"timestamp,omitempty"`
	} `json:"latest_bundle,omitempty"`
	MaintenanceMode      bool `json:"maintenance_mode,omitempty"`
	Multisite            bool `json:"multisite,omitempty"`
	PreviousActiveBundle struct {
		BundlePath string `json:"bundle_path,omitempty"`
		Checksum   string `json:"checksum,omitempty"`
		Timestamp  int    `json:"timestamp,omitempty"`
	} `json:"previous_active_bundle,omitempty"`
	PrimariesBackupStatus   string `json:"primaries_backup_status,omitempty"`
	QuietPeriodFlag         bool   `json:"quiet_period_flag,omitempty"`
	RollingRestartFlag      bool   `json:"rolling_restart_flag,omitempty"`
	RollingRestartOrUpgrade bool   `json:"rolling_restart_or_upgrade,omitempty"`
	ServiceReadyFlag        bool   `json:"service_ready_flag,omitempty"`
	SiteReplicationFactor   string `json:"site_replication_factor,omitempty"`
	SiteSearchFactor        string `json:"site_search_factor,omitempty"`
	StartTime               int    `json:"start_time,omitempty"`
	SummaryReplication      string `json:"summary_replication,omitempty"`
}

type ClusterManagerSiteHeader struct {
	Links struct {
	} `json:"links,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Updated   time.Time `json:"updated,omitempty"`
	Generator struct {
		Build   string `json:"build,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"generator,omitempty"`
	Entry []struct {
		Name    string    `json:"name,omitempty"`
		ID      string    `json:"id,omitempty"`
		Updated time.Time `json:"updated,omitempty"`
		Links   struct {
			Alternate string `json:"alternate,omitempty"`
			List      string `json:"list,omitempty"`
		} `json:"links,omitempty"`
		Author string `json:"author,omitempty"`
		Acl    struct {
			App        string `json:"app,omitempty"`
			CanList    bool   `json:"can_list,omitempty"`
			CanWrite   bool   `json:"can_write,omitempty"`
			Modifiable bool   `json:"modifiable,omitempty"`
			Owner      string `json:"owner,omitempty"`
			Perms      struct {
				Read  []string `json:"read,omitempty"`
				Write []string `json:"write,omitempty"`
			} `json:"perms,omitempty"`
			Removable bool   `json:"removable,omitempty"`
			Sharing   string `json:"sharing,omitempty"`
		} `json:"acl,omitempty"`
		Content ClusterManagerSiteContent `json:"content,omitempty"`
	} `json:"entry,omitempty"`
	Paging struct {
		Total   int `json:"total,omitempty"`
		PerPage int `json:"perPage,omitempty"`
		Offset  int `json:"offset,omitempty"`
	} `json:"paging,omitempty"`
	Messages []interface{} `json:"messages,omitempty"`
}
