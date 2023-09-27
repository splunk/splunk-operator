package manager

import "time"

// https://splunk-cm-cluster-master-service:8089/services/cluster/manager/searchheads?count=0&output_mode=json

type SearchHeadContent struct {
	EaiAcl       interface{} `json:"eai:acl"`
	HostPortPair string      `json:"host_port_pair"`
	Label        string      `json:"label"`
	Site         string      `json:"site"`
	Status       string      `json:"status"`
}

// ClusterMasterSearchHeadsHeader
type ClusterMasterSearchHeadHeader struct {
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
		Content SearchHeadContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
