package cluster

import "time"

// Description: Access cluster node configuration details.
// Rest End point API : services/cluster/config
type ClusterConfigContent struct {
	AccessLoggingForHeartbeats              bool        `json:"access_logging_for_heartbeats"`
	AssignPrimariesToAllSites               string      `json:"assign_primaries_to_all_sites"`
	AutoRebalancePrimaries                  bool        `json:"auto_rebalance_primaries"`
	BucketsToSummarize                      string      `json:"buckets_to_summarize"`
	ClusterLabel                            string      `json:"cluster_label"`
	CmComTimeout                            int         `json:"cm_com_timeout"`
	CmHeartbeatPeriod                       int         `json:"cm_heartbeat_period"`
	CmMaxHbmissCount                        int         `json:"cm_max_hbmiss_count"`
	CxnTimeout                              int         `json:"cxn_timeout"`
	DecommissionForceFinishIdleTime         int         `json:"decommission_force_finish_idle_time"`
	DecommissionForceTimeout                int         `json:"decommission_force_timeout"`
	Disabled                                bool        `json:"disabled"`
	EaiAcl                                  interface{} `json:"eai:acl"`
	EnableParallelAddPeer                   string      `json:"enable_parallel_add_peer"`
	EnablePrimaryFixupDuringMaintenance     string      `json:"enable_primary_fixup_during_maintenance"`
	ExecutorWorkers                         int         `json:"executor_workers"`
	ForwarderSiteFailover                   string      `json:"forwarder_site_failover"`
	ForwarderdataRcvPort                    int         `json:"forwarderdata_rcv_port"`
	ForwarderdataUseSsl                     bool        `json:"forwarderdata_use_ssl"`
	FreezeDuringMaintenance                 string      `json:"freeze_during_maintenance"`
	FrozenNotificationsPerBatch             int         `json:"frozen_notifications_per_batch"`
	GenerationPollInterval                  int         `json:"generation_poll_interval"`
	GUID                                    string      `json:"guid"`
	HeartbeatPeriod                         int64       `json:"heartbeat_period"`
	HeartbeatTimeout                        int         `json:"heartbeat_timeout"`
	LogBucketDuringAddpeer                  string      `json:"log_bucket_during_addpeer"`
	ManagerSwitchoverIdxPing                bool        `json:"manager_switchover_idx_ping"`
	ManagerSwitchoverMode                   string      `json:"manager_switchover_mode"`
	ManagerSwitchoverQuietPeriod            int         `json:"manager_switchover_quiet_period"`
	ManagerURI                              string      `json:"manager_uri"`
	MasterURI                               string      `json:"master_uri"`
	MaxAutoServiceInterval                  int         `json:"max_auto_service_interval"`
	MaxConcurrentPeersJoining               int         `json:"max_concurrent_peers_joining"`
	MaxDelayedUpdatesTimeMs                 int         `json:"max_delayed_updates_time_ms "`
	MaxFixupTimeMs                          int         `json:"max_fixup_time_ms"`
	MaxPeerBuildLoad                        int         `json:"max_peer_build_load"`
	MaxPeerRepLoad                          int         `json:"max_peer_rep_load"`
	MaxPeerSumRepLoad                       int         `json:"max_peer_sum_rep_load"`
	MaxPeersToDownloadBundle                int         `json:"max_peers_to_download_bundle"`
	MaxPrimaryBackupsPerService             int         `json:"max_primary_backups_per_service"`
	MaxRemoveSummaryS2PerService            int         `json:"max_remove_summary_s2_per_service"`
	Mode                                    string      `json:"mode"`
	Multisite                               string      `json:"multisite"`
	NotifyBucketsPeriod                     int         `json:"notify_buckets_period"`
	NotifyScanMinPeriod                     int         `json:"notify_scan_min_period"`
	NotifyScanPeriod                        int         `json:"notify_scan_period"`
	PercentPeersToReload                    int         `json:"percent_peers_to_reload"`
	PercentPeersToRestart                   int         `json:"percent_peers_to_restart"`
	PingFlag                                bool        `json:"ping_flag"`
	PrecompressClusterBundle                bool        `json:"precompress_cluster_bundle"`
	QuietPeriod                             int         `json:"quiet_period"`
	RcvTimeout                              int         `json:"rcv_timeout"`
	RebalancePrimariesExecutionLimitMs      int         `json:"rebalance_primaries_execution_limit_ms"`
	RebalanceThreshold                      float64     `json:"rebalance_threshold"`
	RegisterForwarderAddress                string      `json:"register_forwarder_address"`
	RegisterReplicationAddress              string      `json:"register_replication_address"`
	RegisterSearchAddress                   string      `json:"register_search_address"`
	RemoteStorageRetentionPeriod            int         `json:"remote_storage_retention_period"`
	RemoteStorageUploadTimeout              int         `json:"remote_storage_upload_timeout"`
	RepCxnTimeout                           int         `json:"rep_cxn_timeout"`
	RepMaxRcvTimeout                        int         `json:"rep_max_rcv_timeout"`
	RepMaxSendTimeout                       int         `json:"rep_max_send_timeout"`
	RepRcvTimeout                           int         `json:"rep_rcv_timeout"`
	RepSendTimeout                          int         `json:"rep_send_timeout"`
	ReplicationFactor                       int         `json:"replication_factor"`
	ReplicationPort                         interface{} `json:"replication_port"`
	ReplicationUseSsl                       bool        `json:"replication_use_ssl"`
	ReportingDelayPeriod                    int         `json:"reporting_delay_period"`
	RestartInactivityTimeout                int         `json:"restart_inactivity_timeout"`
	RestartTimeout                          int         `json:"restart_timeout"`
	RollingRestart                          string      `json:"rolling_restart"`
	RollingRestartCondition                 string      `json:"rolling_restart_condition"`
	SearchFactor                            int         `json:"search_factor"`
	SearchFilesRetryTimeout                 int         `json:"search_files_retry_timeout"`
	SearchableRebalance                     string      `json:"searchable_rebalance"`
	SearchableRollingPeerStateDelayInterval int         `json:"searchable_rolling_peer_state_delay_interval"`
	Secret                                  string      `json:"secret"`
	SendTimeout                             int         `json:"send_timeout"`
	ServiceExecutionThresholdMs             int         `json:"service_execution_threshold_ms"`
	ServiceInterval                         int         `json:"service_interval"`
	ServiceJobsMsec                         int         `json:"service_jobs_msec"`
	Site                                    string      `json:"site"`
	SiteBySite                              bool        `json:"site_by_site"`
	SiteReplicationFactor                   string      `json:"site_replication_factor"`
	SiteSearchFactor                        string      `json:"site_search_factor"`
	StreamingReplicationWaitSecs            int         `json:"streaming_replication_wait_secs"`
	SummaryReplication                      string      `json:"summary_replication"`
	SummaryUpdateBatchSize                  int         `json:"summary_update_batch_size"`
	TargetWaitTime                          int         `json:"target_wait_time"`
	UseBatchDiscard                         string      `json:"use_batch_discard"`
	UseBatchMaskChanges                     string      `json:"use_batch_mask_changes"`
	UseBatchRemoteRepChanges                string      `json:"use_batch_remote_rep_changes"`
}

type ClusterConfigHeader struct {
	Links struct {
		Reload string `json:"_reload,omitempty"`
		Acl    string `json:"_acl,omitempty"`
	} `json:"links"`
	Origin    string    `json:"origin,omitempty"`
	Updated   time.Time `json:"updated,omitempty"`
	Generator struct {
		Build   string `json:"build,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"generator"`
	Entry []struct {
		Name    string    `json:"name,omitempty"`
		ID      string    `json:"id,omitempty"`
		Updated time.Time `json:"updated,omitempty"`
		Links   struct {
			Alternate string `json:"alternate,omitempty"`
			List      string `json:"list,omitempty"`
			Reload    string `json:"_reload,omitempty"`
			Edit      string `json:"edit,omitempty"`
			Disable   string `json:"disable,omitempty"`
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
		Content ClusterConfigContent `json:"content,omitempty"`
	} `json:"entry,omitempty"`
	Paging struct {
		Total   int `json:"total,omitempty"`
		PerPage int `json:"perPage,omitempty"`
		Offset  int `json:"offset,omitempty"`
	} `json:"paging,omitempty"`
	Messages []interface{} `json:"messages,omitempty"`
}
