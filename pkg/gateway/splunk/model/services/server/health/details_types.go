package health

import (
	"github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/common"
	"time"
)

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status
type DataForwarding struct {
	Health                  string `json:"health,omitempty"`
	NumRed                  int    `json:"num_red,omitempty"`
	NumYellow               int    `json:"num_yellow,omitempty"`
	Splunk2SplunkForwarding struct {
		Health        string `json:"health,omitempty"`
		NumRed        int    `json:"num_red,omitempty"`
		NumYellow     int    `json:"num_yellow,omitempty"`
		Tcpoutautolb0 struct {
			DisplayName    string `json:"display_name,omitempty"`
			Health         string `json:"health,omitempty"`
			NumRed         int    `json:"num_red,omitempty"`
			NumYellow      int    `json:"num_yellow,omitempty"`
			S2SConnections struct {
				Description string `json:"description,omitempty"`
				Health      string `json:"health,omitempty"`
				Name        string `json:"name,omitempty"`
				Path        string `json:"path,omitempty"`
			} `json:"s2s_connections,omitempty"`
		} `json:"tcpoutautolb-0,omitempty"`
	} `json:"splunk-2-splunk_forwarding,omitempty"`
}

type FileMonitorInput struct {
	ForwarderIngestionLatency struct {
		DisplayName                   string `json:"display_name,omitempty"`
		Health                        string `json:"health,omitempty"`
		IngestionLatencyIndexerHealth struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"ingestion_latency_indexer_health,omitempty"`
		NumRed    int `json:"num_red,omitempty"`
		NumYellow int `json:"num_yellow,omitempty"`
	} `json:"forwarder_ingestion_latency,omitempty"`
	Health           string `json:"health,omitempty"`
	IngestionLatency struct {
		DisplayName                   string `json:"display_name,omitempty"`
		Health                        string `json:"health,omitempty"`
		IngestionLatencyGapMultiplier struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"ingestion_latency_gap_multiplier,omitempty"`
		IngestionLatencyLagSec struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"ingestion_latency_lag_sec,omitempty"`
		NumRed    int `json:"num_red,omitempty"`
		NumYellow int `json:"num_yellow,omitempty"`
	} `json:"ingestion_latency,omitempty"`
	LargeAndArchiveFileReader0 struct {
		DataOutRate struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"data_out_rate,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Health      string `json:"health,omitempty"`
		NumRed      int    `json:"num_red,omitempty"`
		NumYellow   int    `json:"num_yellow,omitempty"`
	} `json:"large_and_archive_file_reader-0,omitempty"`
	LargeAndArchiveFileReader1 struct {
		DataOutRate struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"data_out_rate,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Health      string `json:"health,omitempty"`
		NumRed      int    `json:"num_red,omitempty"`
		NumYellow   int    `json:"num_yellow,omitempty"`
	} `json:"large_and_archive_file_reader-1,omitempty"`
	NumRed          int `json:"num_red,omitempty"`
	NumYellow       int `json:"num_yellow,omitempty"`
	RealTimeReader0 struct {
		DataOutRate struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"data_out_rate,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Health      string `json:"health,omitempty"`
		NumRed      int    `json:"num_red,omitempty"`
		NumYellow   int    `json:"num_yellow,omitempty"`
	} `json:"real-time_reader-0,omitempty"`
	RealTimeReader1 struct {
		DataOutRate struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"data_out_rate,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Health      string `json:"health,omitempty"`
		NumRed      int    `json:"num_red,omitempty"`
		NumYellow   int    `json:"num_yellow,omitempty"`
	} `json:"real-time_reader-1,omitempty"`
}

type IndexProcessor struct {
	Buckets struct {
		BucketsCreatedLast60M struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"buckets_created_last_60m,omitempty"`
		CountBucketRenameFailureLast10Mins struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"count_bucket_rename_failure_last_10mins,omitempty"`
		DisplayName        string `json:"display_name,omitempty"`
		GiganticBucketSize struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"gigantic_bucket_size,omitempty"`
		Health                            string `json:"health,omitempty"`
		NumRed                            int    `json:"num_red,omitempty"`
		NumYellow                         int    `json:"num_yellow,omitempty"`
		PercentSmallBucketsCreatedLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_small_buckets_created_last_24h,omitempty"`
	} `json:"buckets,omitempty"`
	DiskSpace struct {
		DiskSpaceRemainingMultipleMinfreespace struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"disk_space_remaining_multiple_minfreespace,omitempty"`
		DisplayName          string `json:"display_name,omitempty"`
		Health               string `json:"health,omitempty"`
		MaxVolumeSizeInvalid struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"max_volume_size_invalid,omitempty"`
		NumRed    int `json:"num_red,omitempty"`
		NumYellow int `json:"num_yellow,omitempty"`
	} `json:"disk_space,omitempty"`
	Health            string `json:"health,omitempty"`
	IndexOptimization struct {
		ConcurrentOptimizeProcessesPercent struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"concurrent_optimize_processes_percent,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Health      string `json:"health,omitempty"`
		NumRed      int    `json:"num_red,omitempty"`
		NumYellow   int    `json:"num_yellow,omitempty"`
	} `json:"index_optimization,omitempty"`
	NumRed    int `json:"num_red,omitempty"`
	NumYellow int `json:"num_yellow,omitempty"`
}

type ClusterBundles struct {
	ClusterBundles struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"cluster_bundles,omitempty"`
	CountClassicBundleTimeoutLast10Mins struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"count_classic_bundle_timeout_last_10mins,omitempty"`
	CountFullBundleUntarLast10Mins struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"count_full_bundle_untar_last_10mins,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Health      string `json:"health,omitempty"`
	NumRed      int    `json:"num_red,omitempty"`
	NumYellow   int    `json:"num_yellow,omitempty"`
}

type DataDurability struct {
	ClusterReplicationFactor struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"cluster_replication_factor,omitempty"`
	ClusterSearchFactor struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"cluster_search_factor,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Health      string `json:"health,omitempty"`
	NumRed      int    `json:"num_red,omitempty"`
	NumYellow   int    `json:"num_yellow,omitempty"`
}

type DataSearchable struct {
	DataSearchable struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"data_searchable,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Health      string `json:"health,omitempty"`
	NumRed      int    `json:"num_red,omitempty"`
	NumYellow   int    `json:"num_yellow,omitempty"`
}

type Indexers struct {
	CmServiceIntervalInvalid struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"cm_service_interval_invalid,omitempty"`
	Detention struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"detention,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	Health       string `json:"health,omitempty"`
	MissingPeers struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"missing_peers,omitempty"`
	NumRed    int `json:"num_red,omitempty"`
	NumYellow int `json:"num_yellow,omitempty"`
}

type IndexingReady struct {
	DisplayName   string `json:"display_name,omitempty"`
	Health        string `json:"health,omitempty"`
	IndexingReady struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"indexing_ready,omitempty"`
	NumRed    int `json:"num_red,omitempty"`
	NumYellow int `json:"num_yellow,omitempty"`
}

type ManagerConnectivity struct {
	DisplayName        string `json:"display_name,omitempty"`
	Health             string `json:"health,omitempty"`
	MasterConnectivity struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"master_connectivity,omitempty"`
	NumRed    int `json:"num_red,omitempty"`
	NumYellow int `json:"num_yellow,omitempty"`
}

type PeerState struct {
	DisplayName string `json:"display_name,omitempty"`
	Health      string `json:"health,omitempty"`
	NumRed      int    `json:"num_red,omitempty"`
	NumYellow   int    `json:"num_yellow,omitempty"`
	SlaveState  struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"slave_state,omitempty"`
}

type PeerVersion struct {
	DisplayName  string `json:"display_name,omitempty"`
	Health       string `json:"health,omitempty"`
	NumRed       int    `json:"num_red,omitempty"`
	NumYellow    int    `json:"num_yellow,omitempty"`
	SlaveVersion struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"slave_version,omitempty"`
}

type ReplicationFailures struct {
	DisplayName         string `json:"display_name,omitempty"`
	Health              string `json:"health,omitempty"`
	NumRed              int    `json:"num_red,omitempty"`
	NumYellow           int    `json:"num_yellow,omitempty"`
	ReplicationFailures struct {
		Description string `json:"description,omitempty"`
		Health      string `json:"health,omitempty"`
		Name        string `json:"name,omitempty"`
		Path        string `json:"path,omitempty"`
	} `json:"replication_failures,omitempty"`
}

type IndexerClustering struct {
	ClusterBundles      ClusterBundles      `json:"cluster_bundles,omitempty"`
	DataDurability      DataDurability      `json:"data_durability,omitempty"`
	DataSearchable      DataSearchable      `json:"data_searchable,omitempty"`
	Health              string              `json:"health,omitempty"`
	Indexers            Indexers            `json:"indexers,omitempty"`
	IndexingReady       IndexingReady       `json:"indexing_ready,omitempty"`
	ManagerConnectivity ManagerConnectivity `json:"manager_connectivity,omitempty"`
	NumRed              int                 `json:"num_red,omitempty"`
	NumYellow           int                 `json:"num_yellow,omitempty"`
	PeerState           PeerState           `json:"peer_state,omitempty"`
	PeerVersion         PeerVersion         `json:"peer_version,omitempty"`
	ReplicationFailures ReplicationFailures `json:"replication_failures,omitempty"`
}

type ResourceUsage struct {
	Health string `json:"health,omitempty"`
	Iowait struct {
		AvgCPUMaxPercLast3M struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"avg_cpu__max_perc_last_3m,omitempty"`
		DisplayName            string `json:"display_name,omitempty"`
		Health                 string `json:"health,omitempty"`
		NumRed                 int    `json:"num_red,omitempty"`
		NumYellow              int    `json:"num_yellow,omitempty"`
		SingleCPUMaxPercLast3M struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"single_cpu__max_perc_last_3m,omitempty"`
		SumTop3CPUPercsMaxLast3M struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"sum_top3_cpu_percs__max_last_3m,omitempty"`
	} `json:"iowait,omitempty"`
	NumRed    int `json:"num_red,omitempty"`
	NumYellow int `json:"num_yellow,omitempty"`
}

type SearchScheduler struct {
	Health               string `json:"health,omitempty"`
	NumRed               int    `json:"num_red,omitempty"`
	NumYellow            int    `json:"num_yellow,omitempty"`
	SchedulerSuppression struct {
		DisplayName              string `json:"display_name,omitempty"`
		Health                   string `json:"health,omitempty"`
		NumRed                   int    `json:"num_red,omitempty"`
		NumYellow                int    `json:"num_yellow,omitempty"`
		SuppressionListOversized struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"suppression_list_oversized,omitempty"`
	} `json:"scheduler_suppression,omitempty"`
	SearchLag struct {
		CountExtremelyLaggedSearchesLastHour struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"count_extremely_lagged_searches_last_hour,omitempty"`
		DisplayName                              string `json:"display_name,omitempty"`
		Health                                   string `json:"health,omitempty"`
		NumRed                                   int    `json:"num_red,omitempty"`
		NumYellow                                int    `json:"num_yellow,omitempty"`
		PercentSearchesLaggedHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_lagged_high_priority_last_24h,omitempty"`
		PercentSearchesLaggedNonHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_lagged_non_high_priority_last_24h,omitempty"`
	} `json:"search_lag,omitempty"`
	SearchesDelayed struct {
		DisplayName                               string `json:"display_name,omitempty"`
		Health                                    string `json:"health,omitempty"`
		NumRed                                    int    `json:"num_red,omitempty"`
		NumYellow                                 int    `json:"num_yellow,omitempty"`
		PercentSearchesDelayedHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_delayed_high_priority_last_24h,omitempty"`
		PercentSearchesDelayedNonHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_delayed_non_high_priority_last_24h,omitempty"`
	} `json:"searches_delayed,omitempty"`
	SearchesSkippedInTheLast24Hours struct {
		DisplayName                               string `json:"display_name,omitempty"`
		Health                                    string `json:"health,omitempty"`
		NumRed                                    int    `json:"num_red,omitempty"`
		NumYellow                                 int    `json:"num_yellow,omitempty"`
		PercentSearchesSkippedHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_skipped_high_priority_last_24h,omitempty"`
		PercentSearchesSkippedNonHighPriorityLast24H struct {
			Description string `json:"description,omitempty"`
			Health      string `json:"health,omitempty"`
			Name        string `json:"name,omitempty"`
			Path        string `json:"path,omitempty"`
		} `json:"percent_searches_skipped_non_high_priority_last_24h,omitempty"`
	} `json:"searches_skipped_in_the_last_24_hours,omitempty"`
}

type Splunkd struct {
	DataForwarding    DataForwarding    `json:"data_forwarding,omitempty"`
	FileMonitorInput  FileMonitorInput  `json:"file_monitor_input,omitempty"`
	Health            string            `json:"health,omitempty"`
	IndexProcessor    IndexProcessor    `json:"index_processor,omitempty"`
	IndexerClustering IndexerClustering `json:"indexer_clustering,omitempty"`
	NumRed            int               `json:"num_red,omitempty"`
	NumYellow         int               `json:"num_yellow,omitempty"`
	ResourceUsage     ResourceUsage     `json:"resource_usage,omitempty"`
	SearchScheduler   SearchScheduler   `json:"search_scheduler,omitempty"`
}

type Features struct {
	Health    string  `json:"health,omitempty"`
	NumRed    int     `json:"num_red,omitempty"`
	NumYellow int     `json:"num_yellow,omitempty"`
	Splunkd   Splunkd `json:"splunkd,omitempty"`
}

type DeploymentDetail struct {
	Disabled bool        `json:"disabled,omitempty"`
	EaiACL   interface{} `json:"eai:acl,omitempty"`
	Features Features    `json:"features,omitempty"`
	Health   string      `json:"health,omitempty"`
}

type DeploymentDetailHeader struct {
	Links     common.EntryLinks `json:"links,omitempty"`
	Origin    string            `json:"origin,omitempty"`
	Updated   time.Time         `json:"updated,omitempty"`
	Generator common.Generator  `json:"generator,omitempty"`
	Entry     []common.Entry    `json:"entry,omitempty"`
	Paging    common.Paging     `json:"paging,omitempty"`
	Messages  []interface{}     `json:"messages,omitempty"`
}
