package server

import "time"

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status

type IntrospectionHeader struct {
	Links     Links         `json:"links,omitempty"`
	Origin    string        `json:"origin,omitempty"`
	Updated   time.Time     `json:"updated,omitempty"`
	Generator Generator     `json:"generator,omitempty"`
	Entry     []Entry       `json:"entry,omitempty"`
	Paging    Paging        `json:"paging,omitempty"`
	Messages  []interface{} `json:"messages,omitempty"`
}

type NAMING_FAILED struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type NAMING_FAILED0 struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type NAMING_FAILED1 struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type NAMING_FAILED2 struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type NAMING_FAILED3 struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type NAMING_FAILED4 struct {
	Label  string `json:"label,omitempty"`
	Site   string `json:"site,omitempty"`
	Status string `json:"status,omitempty"`
}
type Peers struct {
	NAMING_FAILED  NAMING_FAILED  `json:",omitempty"`
	NAMING_FAILED0 NAMING_FAILED0 `json:",omitempty"`
	NAMING_FAILED1 NAMING_FAILED1 `json:",omitempty"`
	NAMING_FAILED2 NAMING_FAILED2 `json:",omitempty"`
	NAMING_FAILED3 NAMING_FAILED3 `json:",omitempty"`
	NAMING_FAILED4 NAMING_FAILED4 `json:",omitempty"`
}
type RestartProgress struct {
	Done          []interface{} `json:"done,omitempty"`
	Failed        []interface{} `json:"failed,omitempty"`
	InProgress    []interface{} `json:"in_progress,omitempty"`
	ToBeRestarted []interface{} `json:"to_be_restarted,omitempty"`
}
type Introspection struct {
	AvailableSites           string          `json:"available_sites,omitempty"`
	DecommissionForceTimeout string          `json:"decommission_force_timeout,omitempty"`
	EaiACL                   interface{}     `json:"eai:acl,omitempty"`
	HaMode                   string          `json:"ha_mode,omitempty"`
	MaintenanceMode          bool            `json:"maintenance_mode,omitempty"`
	Messages                 string          `json:"messages,omitempty"`
	Multisite                bool            `json:"multisite,omitempty"`
	Peers                    Peers           `json:"peers,omitempty"`
	RestartInactivityTimeout string          `json:"restart_inactivity_timeout,omitempty"`
	RestartProgress          RestartProgress `json:"restart_progress,omitempty"`
	RollingRestartFlag       bool            `json:"rolling_restart_flag,omitempty"`
	RollingRestartOrUpgrade  bool            `json:"rolling_restart_or_upgrade,omitempty"`
	SearchableRolling        bool            `json:"searchable_rolling,omitempty"`
	ServiceReadyFlag         bool            `json:"service_ready_flag,omitempty"`
}
