package manager

import "time"

// Access current generation cluster manager information and create a cluster generation.
// endpoint : https://<host>:<mPort>/services/cluster/manager/generation
type ClusterManagerGenerationContent struct {
	ClusterLabel             string      `json:"cluster_label"`
	EaiAcl                   interface{} `json:"eai:acl"`
	GenerationID             int         `json:"generation_id"`
	GenerationPeers          interface{} `json:"generation_peers"`
	LastCompleteGenerationID int         `json:"last_complete_generation_id"`
	MasterSplunkVersion      string      `json:"master_splunk_version"`
	MultisiteError           string      `json:"multisite_error"`
	NumBuckets               int         `json:"num_buckets"`
	PeersOutOfGeneration     interface{} `json:"peers_out_of_generation"`
	PendingGenerationID      int         `json:"pending_generation_id"`
	PendingLastAttempt       int         `json:"pending_last_attempt"`
	PendingLastReason        string      `json:"pending_last_reason"`
	ReplicationFactorMet     string      `json:"replication_factor_met"`
	SearchFactorMet          string      `json:"search_factor_met"`
	SearchableRolling        bool        `json:"searchable_rolling"`
	WasForced                string      `json:"was_forced"`
}
type ClusterManagerGenerationHeader struct {
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
		Content ClusterManagerGenerationContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
