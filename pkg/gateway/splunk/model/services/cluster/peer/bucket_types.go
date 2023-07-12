package peer

// Access cluster peers bucket configuration.
// List cluster peers bucket configuration.
// endpoint: https://<host>:<mPort>/services/cluster/peer/buckets

type Generations struct {
	Num0 string `json:"0,omitempty"`
}
type ClusterPeerBucket struct {
	Checksum     string      `json:"checksum,omitempty"`
	EaiACL       interface{} `json:"eai:acl,omitempty"`
	EarliestTime int         `json:"earliest_time,omitempty"`
	Generations  Generations `json:"generations,omitempty"`
	Index        string      `json:"index,omitempty"`
	LatestTime   int         `json:"latest_time,omitempty"`
	SearchState  string      `json:"search_state,omitempty"`
	Status       string      `json:"status,omitempty"`
}
