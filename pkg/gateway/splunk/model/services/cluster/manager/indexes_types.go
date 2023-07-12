package manager

import "time"

// Description: Access cluster index information.
// Rest End Point API: services/cluster/manager/indexes
type ReplicatedCopyTracker struct {
	Name                 string `json:"name,omitempty"`
	ActualCopiesPerSlot  string `json:"actual_copies_per_slot"`
	ExpectedTotalPerSlot string `json:"expected_total_per_slot"`
}

type ReplicatedCopiesTracker struct {
	ReplicatedCopiesTracker []ReplicatedCopyTracker `json:"replicated_copy_tracker,omitempty"`
}

type SearchableCopyTracker struct {
	Name                 string `json:"name,omitempty"`
	ActualCopiesPerSlot  string `json:"actual_copies_per_slot,omitempty"`
	ExpectedTotalPerSlot string `json:"expected_total_per_slot,omitempty"`
}

type SearchableCopiesTracker struct {
	SearchableCopyTracker []SearchableCopyTracker `json:"searchable_copy_tracker,omitempty"`
}

type ClusterManagerIndexesContent struct {
	BucketsWithExcessCopies               int         `json:"buckets_with_excess_copies"`
	BucketsWithExcessSearchableCopies     int         `json:"buckets_with_excess_searchable_copies"`
	EaiAcl                                interface{} `json:"eai:acl"`
	IndexSize                             int         `json:"index_size"`
	IsSearchable                          string      `json:"is_searchable"`
	NonSiteAwareBucketsInSiteAwareCluster int         `json:"non_site_aware_buckets_in_site_aware_cluster"`
	NumBuckets                            int         `json:"num_buckets"`
	ReplicatedCopiesTracker               struct {
		Num0 struct {
			ActualCopiesPerSlot  string `json:"actual_copies_per_slot"`
			ExpectedTotalPerSlot string `json:"expected_total_per_slot"`
		} `json:"0"`
		Num1 struct {
			ActualCopiesPerSlot  string `json:"actual_copies_per_slot"`
			ExpectedTotalPerSlot string `json:"expected_total_per_slot"`
		} `json:"1"`
	} `json:"replicated_copies_tracker"`
	SearchableCopiesTracker struct {
		Num0 struct {
			ActualCopiesPerSlot  string `json:"actual_copies_per_slot"`
			ExpectedTotalPerSlot string `json:"expected_total_per_slot"`
		} `json:"0"`
		Num1 struct {
			ActualCopiesPerSlot  string `json:"actual_copies_per_slot"`
			ExpectedTotalPerSlot string `json:"expected_total_per_slot"`
		} `json:"1"`
	} `json:"searchable_copies_tracker"`
	SortOrder                   int `json:"sort_order"`
	TotalExcessBucketCopies     int `json:"total_excess_bucket_copies"`
	TotalExcessSearchableCopies int `json:"total_excess_searchable_copies"`
}

type ClusterManagerIndexesHeader struct {
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
		Content ClusterManagerIndexesContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
