package server

import "time"

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status

type HealthConfig struct {
	Links struct {
		Create string `json:"create"`
		Reload string `json:"_reload"`
		ACL    string `json:"_acl"`
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
			Reload    string `json:"_reload"`
			Edit      string `json:"edit"`
			Disable   string `json:"disable"`
		} `json:"links"`
		Author string `json:"author"`
		ACL    struct {
			App            string `json:"app"`
			CanChangePerms bool   `json:"can_change_perms"`
			CanList        bool   `json:"can_list"`
			CanShareApp    bool   `json:"can_share_app"`
			CanShareGlobal bool   `json:"can_share_global"`
			CanShareUser   bool   `json:"can_share_user"`
			CanWrite       bool   `json:"can_write"`
			Modifiable     bool   `json:"modifiable"`
			Owner          string `json:"owner"`
			Perms          struct {
				Read  []string `json:"read"`
				Write []string `json:"write"`
			} `json:"perms"`
			Removable bool   `json:"removable"`
			Sharing   string `json:"sharing"`
		} `json:"acl"`
		Content struct {
			ActionBcc string      `json:"action.bcc"`
			ActionCc  string      `json:"action.cc"`
			ActionTo  string      `json:"action.to"`
			Disabled  bool        `json:"disabled"`
			EaiACL    interface{} `json:"eai:acl"`
		} `json:"content,omitempty"`
		/*Content0 struct {
			ActionURL string      `json:"action.url"`
			Disabled  bool        `json:"disabled"`
			EaiACL    interface{} `json:"eai:acl"`
		} `json:"content,omitempty"`
		Content1 struct {
			Disabled bool        `json:"disabled"`
			EaiACL   interface{} `json:"eai:acl"`
		} `json:"content,omitempty"`
			Content2 struct {
				AlertDisabled                           string      `json:"alert.disabled"`
				DisplayName                             string      `json:"display_name"`
				EaiACL                                  interface{} `json:"eai:acl"`
				FriendlyDescription                     string      `json:"friendly_description"`
				IndicatorDataOutRateDescription         string      `json:"indicator:data_out_rate:description"`
				IndicatorDataOutRateFriendlyDescription string      `json:"indicator:data_out_rate:friendly_description"`
				IndicatorDataOutRateRed                 string      `json:"indicator:data_out_rate:red"`
				IndicatorDataOutRateYellow              string      `json:"indicator:data_out_rate:yellow"`
			} `json:"content,omitempty"`
			Content3 struct {
				DisplayName                                                    string      `json:"display_name"`
				EaiACL                                                         interface{} `json:"eai:acl"`
				FriendlyDescription                                            string      `json:"friendly_description"`
				IndicatorBucketsCreatedLast60MDescription                      string      `json:"indicator:buckets_created_last_60m:description"`
				IndicatorBucketsCreatedLast60MFriendlyDescription              string      `json:"indicator:buckets_created_last_60m:friendly_description"`
				IndicatorBucketsCreatedLast60MRed                              string      `json:"indicator:buckets_created_last_60m:red"`
				IndicatorBucketsCreatedLast60MYellow                           string      `json:"indicator:buckets_created_last_60m:yellow"`
				IndicatorCountBucketRenameFailureLast10MinsDescription         string      `json:"indicator:count_bucket_rename_failure_last_10mins:description"`
				IndicatorCountBucketRenameFailureLast10MinsFriendlyDescription string      `json:"indicator:count_bucket_rename_failure_last_10mins:friendly_description"`
				IndicatorCountBucketRenameFailureLast10MinsRed                 string      `json:"indicator:count_bucket_rename_failure_last_10mins:red"`
				IndicatorCountBucketRenameFailureLast10MinsYellow              string      `json:"indicator:count_bucket_rename_failure_last_10mins:yellow"`
				IndicatorGiganticBucketSizeDescription                         string      `json:"indicator:gigantic_bucket_size:description"`
				IndicatorGiganticBucketSizeFriendlyDescription                 string      `json:"indicator:gigantic_bucket_size:friendly_description"`
				IndicatorGiganticBucketSizeRed                                 string      `json:"indicator:gigantic_bucket_size:red"`
				IndicatorGiganticBucketSizeYellow                              string      `json:"indicator:gigantic_bucket_size:yellow"`
				IndicatorPercentSmallBucketsCreatedLast24HDescription          string      `json:"indicator:percent_small_buckets_created_last_24h:description"`
				IndicatorPercentSmallBucketsCreatedLast24HFriendlyDescription  string      `json:"indicator:percent_small_buckets_created_last_24h:friendly_description"`
				IndicatorPercentSmallBucketsCreatedLast24HRed                  string      `json:"indicator:percent_small_buckets_created_last_24h:red"`
				IndicatorPercentSmallBucketsCreatedLast24HYellow               string      `json:"indicator:percent_small_buckets_created_last_24h:yellow"`
			} `json:"content,omitempty"`
			Content4 struct {
				DisplayName                                                     string      `json:"display_name"`
				EaiACL                                                          interface{} `json:"eai:acl"`
				FriendlyDescription                                             string      `json:"friendly_description"`
				IndicatorClusterBundlesDescription                              string      `json:"indicator:cluster_bundles:description"`
				IndicatorClusterBundlesFriendlyDescription                      string      `json:"indicator:cluster_bundles:friendly_description"`
				IndicatorClusterBundlesYellow                                   string      `json:"indicator:cluster_bundles:yellow"`
				IndicatorCountClassicBundleTimeoutLast10MinsDescription         string      `json:"indicator:count_classic_bundle_timeout_last_10mins:description"`
				IndicatorCountClassicBundleTimeoutLast10MinsFriendlyDescription string      `json:"indicator:count_classic_bundle_timeout_last_10mins:friendly_description"`
				IndicatorCountClassicBundleTimeoutLast10MinsRed                 string      `json:"indicator:count_classic_bundle_timeout_last_10mins:red"`
				IndicatorCountClassicBundleTimeoutLast10MinsYellow              string      `json:"indicator:count_classic_bundle_timeout_last_10mins:yellow"`
				IndicatorCountFullBundleUntarLast10MinsDescription              string      `json:"indicator:count_full_bundle_untar_last_10mins:description"`
				IndicatorCountFullBundleUntarLast10MinsFriendlyDescription      string      `json:"indicator:count_full_bundle_untar_last_10mins:friendly_description"`
				IndicatorCountFullBundleUntarLast10MinsRed                      string      `json:"indicator:count_full_bundle_untar_last_10mins:red"`
				IndicatorCountFullBundleUntarLast10MinsYellow                   string      `json:"indicator:count_full_bundle_untar_last_10mins:yellow"`
			} `json:"content,omitempty"`
			Content5 struct {
				DisplayName                                          string      `json:"display_name"`
				EaiACL                                               interface{} `json:"eai:acl"`
				FriendlyDescription                                  string      `json:"friendly_description"`
				IndicatorClusterReplicationFactorDescription         string      `json:"indicator:cluster_replication_factor:description"`
				IndicatorClusterReplicationFactorFriendlyDescription string      `json:"indicator:cluster_replication_factor:friendly_description"`
				IndicatorClusterReplicationFactorRed                 string      `json:"indicator:cluster_replication_factor:red"`
				IndicatorClusterSearchFactorDescription              string      `json:"indicator:cluster_search_factor:description"`
				IndicatorClusterSearchFactorFriendlyDescription      string      `json:"indicator:cluster_search_factor:friendly_description"`
				IndicatorClusterSearchFactorRed                      string      `json:"indicator:cluster_search_factor:red"`
			} `json:"content,omitempty"`
			Content6 struct {
				DisplayName                                string      `json:"display_name"`
				EaiACL                                     interface{} `json:"eai:acl"`
				FriendlyDescription                        string      `json:"friendly_description"`
				IndicatorDataSearchableDescription         string      `json:"indicator:data_searchable:description"`
				IndicatorDataSearchableFriendlyDescription string      `json:"indicator:data_searchable:friendly_description"`
				IndicatorDataSearchableRed                 string      `json:"indicator:data_searchable:red"`
			} `json:"content,omitempty"`
			Content7 struct {
				DisplayName                                              string      `json:"display_name"`
				EaiACL                                                   interface{} `json:"eai:acl"`
				FriendlyDescription                                      string      `json:"friendly_description"`
				IndicatorArchivedBucketsFailedLast24HDescription         string      `json:"indicator:archived_buckets_failed_last_24h:description"`
				IndicatorArchivedBucketsFailedLast24HFriendlyDescription string      `json:"indicator:archived_buckets_failed_last_24h:friendly_description"`
				IndicatorArchivedBucketsFailedLast24HRed                 string      `json:"indicator:archived_buckets_failed_last_24h:red"`
				IndicatorArchivedBucketsFailedLast24HYellow              string      `json:"indicator:archived_buckets_failed_last_24h:yellow"`
			} `json:"content,omitempty"`
			Content8 struct {
				DisplayName                                                        string      `json:"display_name"`
				EaiACL                                                             interface{} `json:"eai:acl"`
				FriendlyDescription                                                string      `json:"friendly_description"`
				IndicatorDiskSpaceRemainingMultipleMinfreespaceDescription         string      `json:"indicator:disk_space_remaining_multiple_minfreespace:description"`
				IndicatorDiskSpaceRemainingMultipleMinfreespaceFriendlyDescription string      `json:"indicator:disk_space_remaining_multiple_minfreespace:friendly_description"`
				IndicatorDiskSpaceRemainingMultipleMinfreespaceRed                 string      `json:"indicator:disk_space_remaining_multiple_minfreespace:red"`
				IndicatorDiskSpaceRemainingMultipleMinfreespaceYellow              string      `json:"indicator:disk_space_remaining_multiple_minfreespace:yellow"`
				IndicatorMaxVolumeSizeInvalidDescription                           string      `json:"indicator:max_volume_size_invalid:description"`
				IndicatorMaxVolumeSizeInvalidFriendlyDescription                   string      `json:"indicator:max_volume_size_invalid:friendly_description"`
				IndicatorMaxVolumeSizeInvalidYellow                                string      `json:"indicator:max_volume_size_invalid:yellow"`
			} `json:"content,omitempty"`
			Content9 struct {
				DisplayName                                          string      `json:"display_name"`
				EaiACL                                               interface{} `json:"eai:acl"`
				FriendlyDescription                                  string      `json:"friendly_description"`
				IndicatorCmServiceIntervalInvalidDescription         string      `json:"indicator:cm_service_interval_invalid:description"`
				IndicatorCmServiceIntervalInvalidFriendlyDescription string      `json:"indicator:cm_service_interval_invalid:friendly_description"`
				IndicatorCmServiceIntervalInvalidYellow              string      `json:"indicator:cm_service_interval_invalid:yellow"`
				IndicatorDetentionDescription                        string      `json:"indicator:detention:description"`
				IndicatorDetentionFriendlyDescription                string      `json:"indicator:detention:friendly_description"`
				IndicatorDetentionRed                                string      `json:"indicator:detention:red"`
				IndicatorDetentionYellow                             string      `json:"indicator:detention:yellow"`
				IndicatorMissingPeersDescription                     string      `json:"indicator:missing_peers:description"`
				IndicatorMissingPeersFriendlyDescription             string      `json:"indicator:missing_peers:friendly_description"`
				IndicatorMissingPeersRed                             string      `json:"indicator:missing_peers:red"`
				IndicatorMissingPeersYellow                          string      `json:"indicator:missing_peers:yellow"`
			} `json:"content,omitempty"`
			Content10 struct {
				DisplayName                               string      `json:"display_name"`
				EaiACL                                    interface{} `json:"eai:acl"`
				FriendlyDescription                       string      `json:"friendly_description"`
				IndicatorIndexingReadyDescription         string      `json:"indicator:indexing_ready:description"`
				IndicatorIndexingReadyFriendlyDescription string      `json:"indicator:indexing_ready:friendly_description"`
				IndicatorIndexingReadyRed                 string      `json:"indicator:indexing_ready:red"`
			} `json:"content,omitempty"`
			Content11 struct {
				DisplayName                                               string      `json:"display_name"`
				EaiACL                                                    interface{} `json:"eai:acl"`
				FriendlyDescription                                       string      `json:"friendly_description"`
				IndicatorIngestionLatencyGapMultiplierDescription         string      `json:"indicator:ingestion_latency_gap_multiplier:description"`
				IndicatorIngestionLatencyGapMultiplierFriendlyDescription string      `json:"indicator:ingestion_latency_gap_multiplier:friendly_description"`
				IndicatorIngestionLatencyGapMultiplierRed                 string      `json:"indicator:ingestion_latency_gap_multiplier:red"`
				IndicatorIngestionLatencyGapMultiplierYellow              string      `json:"indicator:ingestion_latency_gap_multiplier:yellow"`
				IndicatorIngestionLatencyLagSecDescription                string      `json:"indicator:ingestion_latency_lag_sec:description"`
				IndicatorIngestionLatencyLagSecFriendlyDescription        string      `json:"indicator:ingestion_latency_lag_sec:friendly_description"`
				IndicatorIngestionLatencyLagSecRed                        string      `json:"indicator:ingestion_latency_lag_sec:red"`
				IndicatorIngestionLatencyLagSecYellow                     string      `json:"indicator:ingestion_latency_lag_sec:yellow"`
			} `json:"content,omitempty"`
			Content12 struct {
				DisplayName                                               string      `json:"display_name"`
				EaiACL                                                    interface{} `json:"eai:acl"`
				FriendlyDescription                                       string      `json:"friendly_description"`
				IndicatorIngestionLatencyIndexerHealthDescription         string      `json:"indicator:ingestion_latency_indexer_health:description"`
				IndicatorIngestionLatencyIndexerHealthFriendlyDescription string      `json:"indicator:ingestion_latency_indexer_health:friendly_description"`
				IndicatorIngestionLatencyIndexerHealthRed                 string      `json:"indicator:ingestion_latency_indexer_health:red"`
				IndicatorIngestionLatencyIndexerHealthYellow              string      `json:"indicator:ingestion_latency_indexer_health:yellow"`
			} `json:"content,omitempty"`
			Content13 struct {
				DisplayName                                          string      `json:"display_name"`
				EaiACL                                               interface{} `json:"eai:acl"`
				FriendlyDescription                                  string      `json:"friendly_description"`
				IndicatorAvgCPUMaxPercLast3MDescription              string      `json:"indicator:avg_cpu__max_perc_last_3m:description"`
				IndicatorAvgCPUMaxPercLast3MFriendlyDescription      string      `json:"indicator:avg_cpu__max_perc_last_3m:friendly_description"`
				IndicatorAvgCPUMaxPercLast3MRed                      string      `json:"indicator:avg_cpu__max_perc_last_3m:red"`
				IndicatorAvgCPUMaxPercLast3MYellow                   string      `json:"indicator:avg_cpu__max_perc_last_3m:yellow"`
				IndicatorSingleCPUMaxPercLast3MDescription           string      `json:"indicator:single_cpu__max_perc_last_3m:description"`
				IndicatorSingleCPUMaxPercLast3MFriendlyDescription   string      `json:"indicator:single_cpu__max_perc_last_3m:friendly_description"`
				IndicatorSingleCPUMaxPercLast3MRed                   string      `json:"indicator:single_cpu__max_perc_last_3m:red"`
				IndicatorSingleCPUMaxPercLast3MYellow                string      `json:"indicator:single_cpu__max_perc_last_3m:yellow"`
				IndicatorSumTop3CPUPercsMaxLast3MDescription         string      `json:"indicator:sum_top3_cpu_percs__max_last_3m:description"`
				IndicatorSumTop3CPUPercsMaxLast3MFriendlyDescription string      `json:"indicator:sum_top3_cpu_percs__max_last_3m:friendly_description"`
				IndicatorSumTop3CPUPercsMaxLast3MRed                 string      `json:"indicator:sum_top3_cpu_percs__max_last_3m:red"`
				IndicatorSumTop3CPUPercsMaxLast3MYellow              string      `json:"indicator:sum_top3_cpu_percs__max_last_3m:yellow"`
			} `json:"content,omitempty"`
			Content14 struct {
				DisplayName                                     string      `json:"display_name"`
				EaiACL                                          interface{} `json:"eai:acl"`
				IndicatorKvstoreDefaultIndexDescription         string      `json:"indicator:kvstore_default_index:description"`
				IndicatorKvstoreDefaultIndexFriendlyDescription string      `json:"indicator:kvstore_default_index:friendly_description"`
				IndicatorKvstoreDefaultIndexYellow              string      `json:"indicator:kvstore_default_index:yellow"`
			} `json:"content,omitempty"`
			Content15 struct {
				DisplayName                                    string      `json:"display_name"`
				EaiACL                                         interface{} `json:"eai:acl"`
				FriendlyDescription                            string      `json:"friendly_description"`
				IndicatorMasterConnectivityDescription         string      `json:"indicator:master_connectivity:description"`
				IndicatorMasterConnectivityFriendlyDescription string      `json:"indicator:master_connectivity:friendly_description"`
				IndicatorMasterConnectivityRed                 string      `json:"indicator:master_connectivity:red"`
			} `json:"content,omitempty"`
			Content16 struct {
				DisplayName                        string      `json:"display_name"`
				EaiACL                             interface{} `json:"eai:acl"`
				FriendlyDescription                string      `json:"friendly_description"`
				IndicatorS2SfRfDescription         string      `json:"indicator:s2_sf_rf:description"`
				IndicatorS2SfRfFriendlyDescription string      `json:"indicator:s2_sf_rf:friendly_description"`
				IndicatorS2SfRfYellow              string      `json:"indicator:s2_sf_rf:yellow"`
			} `json:"content,omitempty"`
			Content17 struct {
				DisplayName                                     string      `json:"display_name"`
				EaiACL                                          interface{} `json:"eai:acl"`
				FriendlyDescription                             string      `json:"friendly_description"`
				IndicatorReplicationFailuresDescription         string      `json:"indicator:replication_failures:description"`
				IndicatorReplicationFailuresFriendlyDescription string      `json:"indicator:replication_failures:friendly_description"`
				IndicatorReplicationFailuresRed                 string      `json:"indicator:replication_failures:red"`
				IndicatorReplicationFailuresYellow              string      `json:"indicator:replication_failures:yellow"`
			} `json:"content,omitempty"`
			Content18 struct {
				DisplayName                                string      `json:"display_name"`
				EaiACL                                     interface{} `json:"eai:acl"`
				FriendlyDescription                        string      `json:"friendly_description"`
				IndicatorS2SConnectionsDescription         string      `json:"indicator:s2s_connections:description"`
				IndicatorS2SConnectionsFriendlyDescription string      `json:"indicator:s2s_connections:friendly_description"`
				IndicatorS2SConnectionsRed                 string      `json:"indicator:s2s_connections:red"`
				IndicatorS2SConnectionsYellow              string      `json:"indicator:s2s_connections:yellow"`
			} `json:"content,omitempty"`
			Content19 struct {
				DisplayName                                          string      `json:"display_name"`
				EaiACL                                               interface{} `json:"eai:acl"`
				IndicatorSuppressionListOversizedDescription         string      `json:"indicator:suppression_list_oversized:description"`
				IndicatorSuppressionListOversizedFriendlyDescription string      `json:"indicator:suppression_list_oversized:friendly_description"`
				IndicatorSuppressionListOversizedYellow              string      `json:"indicator:suppression_list_oversized:yellow"`
			} `json:"content,omitempty"`
			Content20 struct {
				DisplayName                                                             string      `json:"display_name"`
				EaiACL                                                                  interface{} `json:"eai:acl"`
				FriendlyDescription                                                     string      `json:"friendly_description"`
				IndicatorCountExtremelyLaggedSearchesLastHourDescription                string      `json:"indicator:count_extremely_lagged_searches_last_hour:description"`
				IndicatorCountExtremelyLaggedSearchesLastHourFriendlyDescription        string      `json:"indicator:count_extremely_lagged_searches_last_hour:friendly_description"`
				IndicatorCountExtremelyLaggedSearchesLastHourRed                        string      `json:"indicator:count_extremely_lagged_searches_last_hour:red"`
				IndicatorCountExtremelyLaggedSearchesLastHourYellow                     string      `json:"indicator:count_extremely_lagged_searches_last_hour:yellow"`
				IndicatorPercentSearchesLaggedHighPriorityLast24HDescription            string      `json:"indicator:percent_searches_lagged_high_priority_last_24h:description"`
				IndicatorPercentSearchesLaggedHighPriorityLast24HFriendlyDescription    string      `json:"indicator:percent_searches_lagged_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesLaggedHighPriorityLast24HYellow                 string      `json:"indicator:percent_searches_lagged_high_priority_last_24h:yellow"`
				IndicatorPercentSearchesLaggedNonHighPriorityLast24HDescription         string      `json:"indicator:percent_searches_lagged_non_high_priority_last_24h:description"`
				IndicatorPercentSearchesLaggedNonHighPriorityLast24HFriendlyDescription string      `json:"indicator:percent_searches_lagged_non_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesLaggedNonHighPriorityLast24HYellow              string      `json:"indicator:percent_searches_lagged_non_high_priority_last_24h:yellow"`
			} `json:"content,omitempty"`
			Content21 struct {
				DisplayName                                                              string      `json:"display_name"`
				EaiACL                                                                   interface{} `json:"eai:acl"`
				FriendlyDescription                                                      string      `json:"friendly_description"`
				IndicatorPercentSearchesDelayedHighPriorityLast24HDescription            string      `json:"indicator:percent_searches_delayed_high_priority_last_24h:description"`
				IndicatorPercentSearchesDelayedHighPriorityLast24HFriendlyDescription    string      `json:"indicator:percent_searches_delayed_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesDelayedHighPriorityLast24HRed                    string      `json:"indicator:percent_searches_delayed_high_priority_last_24h:red"`
				IndicatorPercentSearchesDelayedHighPriorityLast24HYellow                 string      `json:"indicator:percent_searches_delayed_high_priority_last_24h:yellow"`
				IndicatorPercentSearchesDelayedNonHighPriorityLast24HDescription         string      `json:"indicator:percent_searches_delayed_non_high_priority_last_24h:description"`
				IndicatorPercentSearchesDelayedNonHighPriorityLast24HFriendlyDescription string      `json:"indicator:percent_searches_delayed_non_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesDelayedNonHighPriorityLast24HRed                 string      `json:"indicator:percent_searches_delayed_non_high_priority_last_24h:red"`
				IndicatorPercentSearchesDelayedNonHighPriorityLast24HYellow              string      `json:"indicator:percent_searches_delayed_non_high_priority_last_24h:yellow"`
			} `json:"content,omitempty"`
			Content22 struct {
				DisplayName                                                              string      `json:"display_name"`
				EaiACL                                                                   interface{} `json:"eai:acl"`
				FriendlyDescription                                                      string      `json:"friendly_description"`
				IndicatorPercentSearchesSkippedHighPriorityLast24HDescription            string      `json:"indicator:percent_searches_skipped_high_priority_last_24h:description"`
				IndicatorPercentSearchesSkippedHighPriorityLast24HFriendlyDescription    string      `json:"indicator:percent_searches_skipped_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesSkippedHighPriorityLast24HRed                    string      `json:"indicator:percent_searches_skipped_high_priority_last_24h:red"`
				IndicatorPercentSearchesSkippedHighPriorityLast24HYellow                 string      `json:"indicator:percent_searches_skipped_high_priority_last_24h:yellow"`
				IndicatorPercentSearchesSkippedNonHighPriorityLast24HDescription         string      `json:"indicator:percent_searches_skipped_non_high_priority_last_24h:description"`
				IndicatorPercentSearchesSkippedNonHighPriorityLast24HFriendlyDescription string      `json:"indicator:percent_searches_skipped_non_high_priority_last_24h:friendly_description"`
				IndicatorPercentSearchesSkippedNonHighPriorityLast24HRed                 string      `json:"indicator:percent_searches_skipped_non_high_priority_last_24h:red"`
				IndicatorPercentSearchesSkippedNonHighPriorityLast24HYellow              string      `json:"indicator:percent_searches_skipped_non_high_priority_last_24h:yellow"`
			} `json:"content,omitempty"`
			Content23 struct {
				DisplayName                                            string      `json:"display_name"`
				EaiACL                                                 interface{} `json:"eai:acl"`
				FriendlyDescription                                    string      `json:"friendly_description"`
				IndicatorMasterConnectivityDescription                 string      `json:"indicator:master_connectivity:description"`
				IndicatorMasterConnectivityFriendlyDescription         string      `json:"indicator:master_connectivity:friendly_description"`
				IndicatorMasterConnectivityRed                         string      `json:"indicator:master_connectivity:red"`
				IndicatorMasterVersionCompatibilityDescription         string      `json:"indicator:master_version_compatibility:description"`
				IndicatorMasterVersionCompatibilityFriendlyDescription string      `json:"indicator:master_version_compatibility:friendly_description"`
				IndicatorMasterVersionCompatibilityYellow              string      `json:"indicator:master_version_compatibility:yellow"`
				IndicatorSearchheadPeerConnectivityDescription         string      `json:"indicator:searchhead_peer_connectivity:description"`
				IndicatorSearchheadPeerConnectivityFriendlyDescription string      `json:"indicator:searchhead_peer_connectivity:friendly_description"`
				IndicatorSearchheadPeerConnectivityYellow              string      `json:"indicator:searchhead_peer_connectivity:yellow"`
			} `json:"content,omitempty"`
			Content24 struct {
				DisplayName                                string      `json:"display_name"`
				EaiACL                                     interface{} `json:"eai:acl"`
				FriendlyDescription                        string      `json:"friendly_description"`
				IndicatorCommonBaselineDescription         string      `json:"indicator:common_baseline:description"`
				IndicatorCommonBaselineFriendlyDescription string      `json:"indicator:common_baseline:friendly_description"`
				IndicatorCommonBaselineRed                 string      `json:"indicator:common_baseline:red"`
			} `json:"content,omitempty"`
			Content25 struct {
				DisplayName                                          string      `json:"display_name"`
				EaiACL                                               interface{} `json:"eai:acl"`
				FriendlyDescription                                  string      `json:"friendly_description"`
				IndicatorCaptainBundleReplicationDescription         string      `json:"indicator:captain_bundle_replication:description"`
				IndicatorCaptainBundleReplicationFriendlyDescription string      `json:"indicator:captain_bundle_replication:friendly_description"`
				IndicatorCaptainBundleReplicationYellow              string      `json:"indicator:captain_bundle_replication:yellow"`
				IndicatorCaptainConnectionDescription                string      `json:"indicator:captain_connection:description"`
				IndicatorCaptainConnectionFriendlyDescription        string      `json:"indicator:captain_connection:friendly_description"`
				IndicatorCaptainConnectionRed                        string      `json:"indicator:captain_connection:red"`
				IndicatorCaptainExistenceDescription                 string      `json:"indicator:captain_existence:description"`
				IndicatorCaptainExistenceFriendlyDescription         string      `json:"indicator:captain_existence:friendly_description"`
				IndicatorCaptainExistenceRed                         string      `json:"indicator:captain_existence:red"`
			} `json:"content,omitempty"`
			Content26 struct {
				DisplayName                                      string      `json:"display_name"`
				EaiACL                                           interface{} `json:"eai:acl"`
				FriendlyDescription                              string      `json:"friendly_description"`
				IndicatorDynamicCaptainQuorumDescription         string      `json:"indicator:dynamic_captain_quorum:description"`
				IndicatorDynamicCaptainQuorumFriendlyDescription string      `json:"indicator:dynamic_captain_quorum:friendly_description"`
				IndicatorDynamicCaptainQuorumYellow              string      `json:"indicator:dynamic_captain_quorum:yellow"`
			} `json:"content,omitempty"`
			Content27 struct {
				DisplayName                                   string      `json:"display_name"`
				EaiACL                                        interface{} `json:"eai:acl"`
				FriendlyDescription                           string      `json:"friendly_description"`
				IndicatorDetentionDescription                 string      `json:"indicator:detention:description"`
				IndicatorDetentionFriendlyDescription         string      `json:"indicator:detention:friendly_description"`
				IndicatorDetentionRed                         string      `json:"indicator:detention:red"`
				IndicatorDetentionYellow                      string      `json:"indicator:detention:yellow"`
				IndicatorReplicationFactorDescription         string      `json:"indicator:replication_factor:description"`
				IndicatorReplicationFactorFriendlyDescription string      `json:"indicator:replication_factor:friendly_description"`
				IndicatorReplicationFactorYellow              string      `json:"indicator:replication_factor:yellow"`
				IndicatorStatusDescription                    string      `json:"indicator:status:description"`
				IndicatorStatusFriendlyDescription            string      `json:"indicator:status:friendly_description"`
				IndicatorStatusRed                            string      `json:"indicator:status:red"`
				IndicatorStatusYellow                         string      `json:"indicator:status:yellow"`
			} `json:"content,omitempty"`
			Content28 struct {
				DisplayName                                  string      `json:"display_name"`
				EaiACL                                       interface{} `json:"eai:acl"`
				FriendlyDescription                          string      `json:"friendly_description"`
				IndicatorSnapshotCreationDescription         string      `json:"indicator:snapshot_creation:description"`
				IndicatorSnapshotCreationFriendlyDescription string      `json:"indicator:snapshot_creation:friendly_description"`
				IndicatorSnapshotCreationRed                 string      `json:"indicator:snapshot_creation:red"`
				IndicatorSnapshotCreationYellow              string      `json:"indicator:snapshot_creation:yellow"`
			} `json:"content,omitempty"` */
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
