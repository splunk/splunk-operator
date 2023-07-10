package manager

import (
	"time"
)

// Description: Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
// Rest End Point API: services/cluster/manager/health
type ClusterManagerHealthContent struct {
	AllDataIsSearchable              string      `json:"all_data_is_searchable,omitempty"`
	AllPeersAreUp                    string      `json:"all_peers_are_up,omitempty"`
	CmVersionIsCompatible            string      `json:"cm_version_is_compatible,omitempty"`
	EaiAcl                           interface{} `json:"eai:acl,omitempty"`
	Multisite                        string      `json:"multisite,omitempty"`
	NoFixupTasksInProgress           string      `json:"no_fixup_tasks_in_progress,omitempty"`
	PreFlightCheck                   string      `json:"pre_flight_check,omitempty"`
	ReadyForSearchableRollingRestart string      `json:"ready_for_searchable_rolling_restart,omitempty"`
	ReplicationFactorMet             string      `json:"replication_factor_met,omitempty"`
	SearchFactorMet                  string      `json:"search_factor_met,omitempty"`
	SiteReplicationFactorMet         string      `json:"site_replication_factor_met,omitempty"`
	SiteSearchFactorMet              string      `json:"site_search_factor_met,omitempty"`
	SplunkVersionPeerCount           string      `json:"splunk_version_peer_count,omitempty"`
}

type ClusterManagerHealthHeader struct {
	Links struct {
	} `json:"links,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Updated   time.Time `json:"updated,omitempty"`
	Generator struct {
		Build   string `json:"build,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"generator,omitempty"`
	Entry []struct {
		Name    string    `json:"name,omitempty"`
		ID      string    `json:"id,omitempty"`
		Updated time.Time `json:"updated,omitempty"`
		Links   struct {
			Alternate string `json:"alternate,omitempty"`
			List      string `json:"list,omitempty"`
		} `json:"links,omitempty"`
		Author string `json:"author,omitempty"`
		Acl    struct {
			App        string `json:"app,omitempty"`
			CanList    bool   `json:"can_list,omitempty"`
			CanWrite   bool   `json:"can_write,omitempty"`
			Modifiable bool   `json:"modifiable,omitempty"`
			Owner      string `json:"owner,omitempty"`
			Perms      struct {
				Read  []string `json:"read,omitempty"`
				Write []string `json:"write,omitempty"`
			} `json:"perms,omitempty"`
			Removable bool   `json:"removable,omitempty"`
			Sharing   string `json:"sharing,omitempty"`
		} `json:"acl,omitempty"`
		Content ClusterManagerHealthContent `json:"content,omitempty"`
	} `json:"entry,omitempty"`
	Paging struct {
		Total   int `json:"total,omitempty"`
		PerPage int `json:"perPage,omitempty"`
		Offset  int `json:"offset,omitempty"`
	} `json:"paging,omitempty"`
	Messages []interface{} `json:"messages,omitempty"`
}
