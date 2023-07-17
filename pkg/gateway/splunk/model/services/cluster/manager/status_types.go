package manager

import "time"

// Description: Endpoint to get the status of a rolling restart.
// Rest End Point: services/cluster/manager/status
type ClusterManagerStatusContent struct {
	AvailableSites           string      `json:"available_sites"`
	DecommissionForceTimeout string      `json:"decommission_force_timeout"`
	EaiAcl                   interface{} `json:"eai:acl"`
	HaMode                   string      `json:"ha_mode"`
	MaintenanceMode          bool        `json:"maintenance_mode"`
	Messages                 string      `json:"messages"`
	Multisite                bool        `json:"multisite"`
	Peers                    struct {
		One88C23DDD6414BA2B651C042F809A0B3 struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"188C23DD-D641-4BA2-B651-C042F809A0B3"`
		OneFBC4C960AD04C0084684DDA988FB808 struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"1FBC4C96-0AD0-4C00-8468-4DDA988FB808"`
		ThreeA617349B0774E0FB76A41C300B00326 struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"3A617349-B077-4E0F-B76A-41C300B00326"`
		SevenD3E85ABB17A47A6B5E9405FB889AD25 struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"7D3E85AB-B17A-47A6-B5E9-405FB889AD25"`
		CB87DA8D38FF42D8B7EC076C97D77E18 struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"CB87DA8D-38FF-42D8-B7EC-076C97D77E18"`
		F881BA5FE1814C09BB3396131460678E struct {
			Label  string `json:"label"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"F881BA5F-E181-4C09-BB33-96131460678E"`
	} `json:"peers"`
	RestartInactivityTimeout string `json:"restart_inactivity_timeout"`
	RestartProgress          struct {
		Done          []interface{} `json:"done"`
		Failed        []interface{} `json:"failed"`
		InProgress    []interface{} `json:"in_progress"`
		ToBeRestarted []interface{} `json:"to_be_restarted"`
	} `json:"restart_progress"`
	RollingRestartFlag      bool `json:"rolling_restart_flag"`
	RollingRestartOrUpgrade bool `json:"rolling_restart_or_upgrade"`
	SearchableRolling       bool `json:"searchable_rolling"`
	ServiceReadyFlag        bool `json:"service_ready_flag"`
}

type ClusterManagerStatusHeader struct {
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
		Content ClusterManagerStatusContent `json:"content"`
	} `json:"entry"`
	Paging struct {
		Total   int `json:"total"`
		PerPage int `json:"perPage"`
		Offset  int `json:"offset"`
	} `json:"paging"`
	Messages []interface{} `json:"messages"`
}
