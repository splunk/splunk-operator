package searchhead

import "time"

// Description:  Access peer information in a cluster searchhead.
// Rest Api End Point: services/cluster/searchhead/generation

type SearchHeadPeersHeader struct {
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
		ACL    struct {
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
		Content struct {
			ClusterLabel         string      `json:"cluster_label"`
			ClusterMasterVersion string      `json:"cluster_master_version"`
			EaiACL               interface{} `json:"eai:acl"`
			GenerationError      string      `json:"generation_error"`
			GenerationID         string      `json:"generation_id"`
			GenerationPeers      struct {
				NAMING_FAILED struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
				NAMING_FAILED0 struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
				NAMING_FAILED1 struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
				NAMING_FAILED2 struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
				NAMING_FAILED3 struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
				NAMING_FAILED4 struct {
					HostPortPair string `json:"host_port_pair"`
					Peer         string `json:"peer"`
					Site         string `json:"site"`
					Status       string `json:"status"`
				} `json:""`
			} `json:"generation_peers"`
			IsSearchable         string      `json:"is_searchable"`
			MultisiteError       bool        `json:"multisite_error"`
			PeersOutOfGeneration interface{} `json:"peers_out_of_generation"`
			ReplicationFactorMet string      `json:"replication_factor_met"`
			SearchFactorMet      string      `json:"search_factor_met"`
			SearchableRolling    bool        `json:"searchable_rolling"`
			Status               string      `json:"status"`
			WasForced            bool        `json:"was_forced"`
		} `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
