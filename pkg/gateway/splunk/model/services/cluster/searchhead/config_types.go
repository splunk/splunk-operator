package searchhead

import "time"

// Description:  Access cluster searchhead node configuration.
// Rest Api End Point: services/cluster/searchhead/searchheadconfig

type ConfigHeader struct {
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
			Remove    string `json:"remove"`
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
			EaiACL     interface{} `json:"eai:acl"`
			ManagerURI string      `json:"manager_uri"`
			MasterURI  string      `json:"master_uri"`
			MultiSite  string      `json:"multiSite"`
			Secret     string      `json:"secret"`
			Site       string      `json:"site"`
		} `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
