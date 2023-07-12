package server

import "time"

// SystemInfo
// Exposes relevant information about the resources and OS settings of the machine where Splunk Enterprise is running.
// Rest API End Point: /services/server/sysinfo
type SystemInfoHeader struct {
	Links     Links         `json:"links,omitempty"`
	Origin    string        `json:"origin,omitempty"`
	Updated   time.Time     `json:"updated,omitempty"`
	Generator Generator     `json:"generator,omitempty"`
	Entry     []Entry       `json:"entry,omitempty"`
	Paging    Paging        `json:"paging,omitempty"`
	Messages  []interface{} `json:"messages,omitempty"`
}

type Generator struct {
	Build   string `json:"build,omitempty"`
	Version string `json:"version,omitempty"`
}
type Links struct {
	Alternate string `json:"alternate,omitempty"`
	List      string `json:"list,omitempty"`
}
type Perms struct {
	Read  []string      `json:"read,omitempty"`
	Write []interface{} `json:"write,omitempty"`
}
type ACL struct {
	App        string `json:"app,omitempty"`
	CanList    bool   `json:"can_list,omitempty"`
	CanWrite   bool   `json:"can_write,omitempty"`
	Modifiable bool   `json:"modifiable,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Perms      Perms  `json:"perms,omitempty"`
	Removable  bool   `json:"removable,omitempty"`
	Sharing    string `json:"sharing,omitempty"`
}
type TransparentHugepages struct {
	Defrag         string `json:"defrag,omitempty"`
	EffectiveState string `json:"effective_state,omitempty"`
	Enabled        string `json:"enabled,omitempty"`
}
type Ulimits struct {
	CoreFileSize            int `json:"core_file_size,omitempty"`
	CPUTime                 int `json:"cpu_time,omitempty"`
	DataFileSize            int `json:"data_file_size,omitempty"`
	DataSegmentSize         int `json:"data_segment_size,omitempty"`
	Nice                    int `json:"nice,omitempty"`
	OpenFiles               int `json:"open_files,omitempty"`
	ResidentMemorySize      int `json:"resident_memory_size,omitempty"`
	StackSize               int `json:"stack_size,omitempty"`
	UserProcesses           int `json:"user_processes,omitempty"`
	VirtualAddressSpaceSize int `json:"virtual_address_space_size,omitempty"`
}

type SystemInfo struct {
	CPUArch              string               `json:"cpu_arch,omitempty"`
	EaiACL               interface{}          `json:"eai:acl,omitempty"`
	NumberOfCores        int                  `json:"numberOfCores,omitempty"`
	NumberOfVirtualCores int                  `json:"numberOfVirtualCores,omitempty"`
	OsBuild              string               `json:"os_build,omitempty"`
	OsName               string               `json:"os_name,omitempty"`
	OsNameExtended       string               `json:"os_name_extended,omitempty"`
	OsVersion            string               `json:"os_version,omitempty"`
	PhysicalMemoryMB     int                  `json:"physicalMemoryMB,omitempty"`
	TransparentHugepages TransparentHugepages `json:"transparent_hugepages,omitempty"`
	Ulimits              Ulimits              `json:"ulimits,omitempty"`
}

type Entry struct {
	Name    string      `json:"name,omitempty"`
	ID      string      `json:"id,omitempty"`
	Updated time.Time   `json:"updated,omitempty"`
	Links   Links       `json:"links,omitempty"`
	Author  string      `json:"author,omitempty"`
	ACL     ACL         `json:"acl,omitempty"`
	Content interface{} `json:"content,omitempty"`
}
type Paging struct {
	Total   int `json:"total,omitempty"`
	PerPage int `json:"perPage,omitempty"`
	Offset  int `json:"offset,omitempty"`
}
