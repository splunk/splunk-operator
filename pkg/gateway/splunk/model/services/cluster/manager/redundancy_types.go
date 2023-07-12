package manager

import "time"

// Description: Display the details of all cluster managers participating in cluster manager redundancy, and switch the HA state of the cluster managers.
// Rest End Point: cluster/manager/redundancy
type ClusterManagerRedundancyContent struct {
	EaiACL interface{} `json:"eai:acl"`
	Peers  struct {
		NAMING_FAILED struct {
			HostPortPair string `json:"host_port_pair"`
			ServerName   string `json:"server_name"`
		} `json:""`
		NAMING_FAILED0 struct {
			HostPortPair string `json:"host_port_pair"`
			ServerName   string `json:"server_name"`
		} `json:""`
		NAMING_FAILED1 struct {
			HostPortPair string `json:"host_port_pair"`
			ServerName   string `json:"server_name"`
		} `json:""`
	} `json:"peers"`
}

type ClusterManagerRedundancyHeader struct {
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
		Content ClusterManagerRedundancyContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
