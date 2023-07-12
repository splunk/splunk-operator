package status

import "time"

// Description : Access operating system resource utilization information.
// rest end point : server/status/resource-usage/splunk-processes

type SplunkProcessResourceUsage struct {
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
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		/*Content0 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		Content1 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		Content2 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		Content3 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		Content4 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"`
		Content5 struct {
			Args             string      `json:"args"`
			EaiACL           interface{} `json:"eai:acl"`
			Elapsed          string      `json:"elapsed"`
			FdUsed           string      `json:"fd_used"`
			MemUsed          string      `json:"mem_used"`
			NormalizedPctCPU string      `json:"normalized_pct_cpu"`
			PageFaults       string      `json:"page_faults"`
			PctCPU           string      `json:"pct_cpu"`
			PctMemory        string      `json:"pct_memory"`
			Pid              string      `json:"pid"`
			Ppid             string      `json:"ppid"`
			Process          string      `json:"process"`
			ProcessType      string      `json:"process_type"`
			ReadMb           string      `json:"read_mb"`
			Status           string      `json:"status"`
			TCount           string      `json:"t_count"`
			WrittenMb        string      `json:"written_mb"`
		} `json:"content,omitempty"` */
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
