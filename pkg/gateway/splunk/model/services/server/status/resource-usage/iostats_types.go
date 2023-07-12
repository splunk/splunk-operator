package status

import "time"

// Description:  Access the most recent disk I/O statistics for each disk. This endpoint is
// currently supported for Linux, Windows, and Solaris. By default this endpoint is updated
// every 60s seconds.
// rest end point : /services/server/status/resource-usage/iostats

type IOStats struct {
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
			AvgServiceMs string      `json:"avg_service_ms"`
			AvgTotalMs   string      `json:"avg_total_ms"`
			CPUPct       string      `json:"cpu_pct"`
			Device       string      `json:"device"`
			EaiACL       interface{} `json:"eai:acl"`
			Interval     string      `json:"interval"`
			ReadsKbPs    string      `json:"reads_kb_ps"`
			ReadsPs      string      `json:"reads_ps"`
			WritesKbPs   string      `json:"writes_kb_ps"`
			WritesPs     string      `json:"writes_ps"`
		} `json:"content,omitempty"`
		/*Content0 struct {
			AvgServiceMs string      `json:"avg_service_ms"`
			AvgTotalMs   string      `json:"avg_total_ms"`
			CPUPct       string      `json:"cpu_pct"`
			Device       string      `json:"device"`
			EaiACL       interface{} `json:"eai:acl"`
			FsType       string      `json:"fs_type"`
			Interval     string      `json:"interval"`
			MountPoint   string      `json:"mount_point"`
			ReadsKbPs    string      `json:"reads_kb_ps"`
			ReadsPs      string      `json:"reads_ps"`
			WritesKbPs   string      `json:"writes_kb_ps"`
			WritesPs     string      `json:"writes_ps"`
		} `json:"content,omitempty"` */
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
