package health

import "time"

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status

type DeploymentHeader struct {
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
			Details   string `json:"details"`
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
		Fields struct {
			Required []interface{} `json:"required"`
			Optional []interface{} `json:"optional"`
			Wildcard []interface{} `json:"wildcard"`
		} `json:"fields"`
		Content struct {
			Disabled bool        `json:"disabled"`
			EaiACL   interface{} `json:"eai:acl"`
			Health   string      `json:"health"`
		} `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
