package status

import "time"

// Description: Access search job information.
// Rest End Point : /services/server/status/dispatch-artifacts

type DispatchArtifacts struct {
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
				Read  []string      `json:"read"`
				Write []interface{} `json:"write"`
			} `json:"perms"`
			Removable bool   `json:"removable"`
			Sharing   string `json:"sharing"`
		} `json:"acl"`
		Content struct {
			EaiACL  interface{} `json:"eai:acl"`
			TopApps struct {
				Num0 struct {
					Search string `json:"search"`
				} `json:"0"`
			} `json:"top_apps"`
			TopNamedSearches interface{} `json:"top_named_searches"`
			TopUsers         struct {
				Num0 struct {
					SplunkSystemUser string `json:"splunk-system-user"`
				} `json:"0"`
			} `json:"top_users"`
			TotalCount string `json:"total_count"`
		} `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
