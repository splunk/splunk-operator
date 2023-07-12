package health

import (
	"github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/common"
	"time"
)

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status

type SplunkdDetails struct {
	EaiACL   interface{} `json:"eai:acl"`
	Features struct {
		DataForwarding struct {
			Features struct {
				Splunk2SplunkForwarding struct {
					Features struct {
						TCPOutAutoLB0 struct {
							Health string `json:"health"`
						} `json:"TCPOutAutoLB-0"`
					} `json:"features"`
					Health string `json:"health"`
				} `json:"Splunk-2-Splunk Forwarding"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"Data Forwarding"`
		FileMonitorInput struct {
			Features struct {
				ForwarderIngestionLatency struct {
					Health string `json:"health"`
				} `json:"Forwarder Ingestion Latency"`
				IngestionLatency struct {
					Health string `json:"health"`
				} `json:"Ingestion Latency"`
				LargeAndArchiveFileReader0 struct {
					Health string `json:"health"`
				} `json:"Large and Archive File Reader-0"`
				RealTimeReader0 struct {
					Health string `json:"health"`
				} `json:"Real-time Reader-0"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"File Monitor Input"`
		IndexProcessor struct {
			Features struct {
				Buckets struct {
					Health string `json:"health"`
				} `json:"Buckets"`
				DiskSpace struct {
					Health string `json:"health"`
				} `json:"Disk Space"`
				IndexOptimization struct {
					Health string `json:"health"`
				} `json:"Index Optimization"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"Index Processor"`
		IndexerClustering struct {
			Features struct {
				ClusterBundles struct {
					Health string `json:"health"`
				} `json:"Cluster Bundles"`
				DataDurability struct {
					Health string `json:"health"`
				} `json:"Data Durability"`
				DataSearchable struct {
					Health string `json:"health"`
				} `json:"Data Searchable"`
				Indexers struct {
					Health string `json:"health"`
				} `json:"Indexers"`
				IndexingReady struct {
					Health string `json:"health"`
				} `json:"Indexing Ready"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"Indexer Clustering"`
		ResourceUsage struct {
			Features struct {
				IOWait struct {
					Health string `json:"health"`
				} `json:"IOWait"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"Resource Usage"`
		SearchScheduler struct {
			Features struct {
				SchedulerSuppression struct {
					Health string `json:"health"`
				} `json:"Scheduler Suppression"`
				SearchLag struct {
					Health string `json:"health"`
				} `json:"Search Lag"`
				SearchesDelayed struct {
					Health string `json:"health"`
				} `json:"Searches Delayed"`
				SearchesSkippedInTheLast24Hours struct {
					Health string `json:"health"`
				} `json:"Searches Skipped in the last 24 hours"`
			} `json:"features"`
			Health string `json:"health"`
		} `json:"Search Scheduler"`
		WorkloadManagement struct {
			Disabled bool   `json:"disabled"`
			Health   string `json:"health"`
		} `json:"Workload Management"`
	} `json:"features"`
	Health string `json:"health"`
}

type SplunkdDetailsHeader struct {
	Links     common.EntryLinks `json:"links"`
	Origin    string            `json:"origin"`
	Updated   time.Time         `json:"updated"`
	Generator common.Generator  `json:"generator"`
	Entry     common.Entry      `json:"entry"`
	Paging    common.Paging     `json:"paging"`
	Messages  []interface{}     `json:"messages"`
}
