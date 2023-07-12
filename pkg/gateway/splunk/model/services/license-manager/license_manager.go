package licensemanager

import "time"

// endpoint: "https://localhost:8089/services/licenser/localpeer?output_mode=json"
type LicenseLocalPeerEntry struct {
	Name    string `json:"name,omitempty"`
	ID      string `json:"id,omitempty"`
	Content struct {
		GUID                     []string `json:"guid,omitempty"`
		LastTrackerdbServiceTime int      `json:"last_trackerdb_service_time,omitempty"`
		LicenseKeys              []string `json:"license_keys,omitempty"`
		MasterGUID               string   `json:"master_guid,omitempty"`
		MasterURI                string   `json:"master_uri,omitempty"`
	} `json:"content,omitempty"`
}

type LicenseLocalPeerHeader struct {
	Links struct {
	} `json:"links,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Updated   time.Time `json:"updated,omitempty"`
	Generator struct {
		Build   string `json:"build,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"generator,omitempty"`
	Entry  []LicenseLocalPeerEntry `json:"entry,omitempty"`
	Paging struct {
		Total   int `json:"total,omitempty"`
		PerPage int `json:"perPage,omitempty"`
		Offset  int `json:"offset,omitempty"`
	} `json:"paging,omitempty"`
	Messages []interface{} `json:"messages,omitempty"`
}
