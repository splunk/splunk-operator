package status

import "time"

// Description: Access host-level dynamic CPU utilization and paging information.
// rest end point : services/server/status/resource-usage/hostwide?output_mode=json

type HostwideResourceUsage struct {
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
			CPUArch               string      `json:"cpu_arch"`
			CPUCount              string      `json:"cpu_count"`
			CPUIdlePct            string      `json:"cpu_idle_pct"`
			CPUSystemPct          string      `json:"cpu_system_pct"`
			CPUUserPct            string      `json:"cpu_user_pct"`
			EaiACL                interface{} `json:"eai:acl"`
			Forks                 string      `json:"forks"`
			InstanceGUID          string      `json:"instance_guid"`
			Mem                   string      `json:"mem"`
			MemUsed               string      `json:"mem_used"`
			NormalizedLoadAvg1Min string      `json:"normalized_load_avg_1min"`
			OsBuild               string      `json:"os_build"`
			OsName                string      `json:"os_name"`
			OsNameExt             string      `json:"os_name_ext"`
			OsVersion             string      `json:"os_version"`
			PgPagedOut            string      `json:"pg_paged_out"`
			PgSwappedOut          string      `json:"pg_swapped_out"`
			RunnableProcessCount  string      `json:"runnable_process_count"`
			SplunkVersion         string      `json:"splunk_version"`
			Swap                  string      `json:"swap"`
			SwapUsed              string      `json:"swap_used"`
			VirtualCPUCount       string      `json:"virtual_cpu_count"`
		} `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
