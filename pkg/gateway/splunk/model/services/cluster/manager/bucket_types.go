package manager

import "time"

// Description: Provides bucket configuration information for a cluster manager node.
// Rest Api End Point: services/cluster/manager/buckets
// GenerationPeer
type GenerationPeer struct {
	Name         string `json:"name,omitempty"`
	HostPortPair string `json:"host_port_pair,omitempty"`
	Peer         string `json:"peer,omitempty"`
	Site         string `json:"site,omitempty"`
	Status       string `json:"status,omitempty"`
}

type ClusterManagerBucketContent struct {
	ClusterLabel             string      `json:"cluster_label,omitempty"`
	EaiAcl                   interface{} `json:"eai:acl,omitempty"`
	GenerationID             int         `json:"generation_id,omitempty"`
	GenerationPeers          interface{} `json:"generation_peers,omitempty"`
	LastCompleteGenerationID int         `json:"last_complete_generation_id,omitempty"`
	MasterSplunkVersion      string      `json:"master_splunk_version,omitempty"`
	MultisiteError           string      `json:"multisite_error,omitempty"`
	NumBuckets               int         `json:"num_buckets,omitempty"`
	PeersOutOfGeneration     interface{} `json:"peers_out_of_generation,omitempty"`
	PendingGenerationID      int         `json:"pending_generation_id,omitempty"`
	PendingLastAttempt       int         `json:"pending_last_attempt,omitempty"`
	PendingLastReason        string      `json:"pending_last_reason,omitempty"`
	ReplicationFactorMet     string      `json:"replication_factor_met,omitempty"`
	SearchFactorMet          string      `json:"search_factor_met,omitempty"`
	SearchableRolling        bool        `json:"searchable_rolling,omitempty"`
	WasForced                string      `json:"was_forced,omitempty"`
}

// BucketHeader
type ClusterManagerBucketHeader struct {
	Links struct {
		Create string `json:"create,omitempty"`
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
			Edit      string `json:"edit,omitempty"`
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
		Content ClusterManagerBucketContent `json:"content,omitempty"`
	} `json:"entry,omitempty"`
	Paging struct {
		Total   int `json:"total,omitempty"`
		PerPage int `json:"perPage,omitempty"`
		Offset  int `json:"offset,omitempty"`
	} `json:"paging,omitempty"`
	Messages []interface{} `json:"messages,omitempty"`
}
