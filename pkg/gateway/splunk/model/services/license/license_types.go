package license

// https://<host>:<mPort>/services/licenser/groups
// Provides access to the configuration of licenser groups.
// A licenser group contains one or more licenser stacks that can operate concurrently.
// Only one licenser group is active at any given time.
type LicenseGroup struct {
	IsActive string   `json:"is_active,omitempty"`
	StackIds []string `json:"stack_ids,omitempty"`
}

// https://<host>:<mPort>/services/licenser/licenses
// Provides access to the licenses for this Splunk Enterprise instance.
// A license enables various features for a Splunk instance, including but not limited
// to indexing quota, auth, search, forwarding.
type License struct {
	AddOns                     string   `json:"add_ons,omitempty"`
	AllowedRoles               []string `json:"allowedRoles,omitempty"`
	AssignableRoles            []string `json:"assignableRoles,omitempty"`
	CreationTime               uint     `json:"creation_time,omitempty"`
	DisabledFeatures           []string `json:"disabled_features,omitempty"`
	ExpirationTime             int      `json:"expiration_time,omitempty"`
	Features                   []string `json:"features,omitempty"`
	GroupId                    string   `json:"group_id,omitempty"`
	Guid                       string   `json:"guid,omitempty"`
	IsUnlimited                bool     `json:"is_unlimited,omitempty"`
	Label                      string   `json:"label,omitempty"`
	LicenseHash                string   `json:"license_hash,omitempty"`
	MaxRetentionSize           int      `json:"max_retention_size,omitempty"`
	MaxStackQuota              float64  `json:"max_stack_quota,omitempty"`
	MaxUsers                   int      `json:"max_users,omitempty"`
	MaxViolation               int      `json:"max_violations,omitempty"`
	Notes                      string   `json:"notes,omitempty"`
	Quota                      int      `json:"quota,omitempty"`
	RelativeExpirationInterval int      `json:"relative_expiration_interval,omitempty"`
	RelativeExpirationStart    int      `json:"relative_expiration_start,omitempty"`
	SourceTypes                []string `json:"sourcetypes,omitempty"`
	StackId                    string   `json:"stack_id,omitempty"`
	Status                     string   `json:"status,omitempty"`
	SubGroupId                 string   `json:"subgroup_id,omitempty"`
	Type                       string   `json:"type,omitempty"`
	WindowPeriod               int      `json:"window_period,omitempty"`
}

type Features struct {
	AWSMarketPlace                  string `json:"AWSMarketplace,omitempty"`
	Acceleration                    string `json:"Acceleration,omitempty"`
	AdvancedSearchCommands          string `json:"AdvancedSearchCommands,omitempty"`
	AdvanceXML                      string `json:"AdvancedXML,omitempty"`
	Alerting                        string `json:"Alerting,omitempty"`
	AllowDuplicateKeys              string `json:"AllowDuplicateKeys,omitempty"`
	ArchiveToHdfs                   string `json:"ArchiveToHdfs,omitempty"`
	Auth                            string `json:"Auth,omitempty"`
	CanBeRemoteMaster               string `json:"CanBeRemoteMaster,omitempty"`
	ConditionalLicensingEnforcement string `json:"ConditionalLicensingEnforcement,omitempty"`
	CustomRoles                     string `json:"CustomRoles,omitempty"`
	DeployClient                    string `json:"DeployClient,omitempty"`
	DeployServer                    string `json:"DeployServer,omitempty"`
	DisableQuotaEnforcement         string `json:"DisableQuotaEnforcement,omitempty"`
	DistSearch                      string `json:"DistSearch,omitempty"`
	FwdData                         string `json:"FwdData,omitempty"`
	GuestPass                       string `json:"GuestPass,omitempty"`
	HideQuotaWarning                string `json:"HideQuotaWarnings"`
	KVStore                         string `json:"KVStore,omitempty"`
	LDAPAuth                        string `json:"LDAPAuth,omitempty"`
	LocalSearch                     string `json:"LocalSearch,omitempty"`
	MultifactorAuth                 string `json:"MultifactorAuth,omitempty"`
	MultisiteClustering             string `json:"MultisiteClustering,omitempty"`
	NontableLookups                 string `json:"NontableLookups,omitempty"`
	RcvData                         string `json:"RcvData,omitempty"`
	RcvSearch                       string `json:"RcvSearch,omitempty"`
	ResetWarning                    string `json:"ResetWarnings,omitempty"`
	RollingWindowAlert              string `json:"RollingWindowAlerts,omitempty"`
	SAMLAuth                        string `json:"SAMLAuth,omitempty"`
	ScheduledAlert                  string `json:"ScheduledAlerts,omitempty"`
	ScheduledReports                string `json:"ScheduledReports,omitempty"`
	ScheduledSearch                 string `json:"ScheduledSearch,omitempty"`
	ScriptedAuth                    string `json:"ScriptedAuth,omitempty"`
	SearchheadPooling               string `json:"SearchheadPooling,omitempty"`
	SigningProcessor                string `json:"SigningProcessor,omitempty"`
	SplunkWeb                       string `json:"SplunkWeb,omitempty"`
	SubgroupId                      string `json:"SubgroupId,omitempty"`
	SyslogOutputProcessor           string `json:"SyslogOutputProcessor,omitempty"`
	UnisiteClustring                string `json:"UnisiteClustering,omitempty"`
}

// https://<host>:<mPort>/services/licenser/localpeer
// Get license state information for the Splunk instance.
type LicenseLocalPeer struct {
	AddOns                        string   `json:"add_ons,omitempty"`
	ConnectionTimeout             int      `json:"connection_timeout,omitempty"`
	Features                      Features `json:"features,omitempty"`
	Guid                          []string `json:"guid"`
	LastManagerContactAttemptTime int      `json:"last_manager_contact_attempt_time,omitempty"`
	LastManagerContactSuccessTime int      `json:"last_manager_contact_success_time,omitempty"`
	LastTrackDBServiceTime        int      `json:"last_trackerdb_service_time,omitempty"`
	LicenseKeys                   []string `json:"license_keys,omitempty"`
	ManagerGuid                   string   `json:"manager_guid,omitempty"`
	ManagerUri                    string   `json:"manager_uri,omitempty"`
	PeerId                        string   `json:"peer_id,omitempty"`
	PeerLabel                     string   `json:"peer_label,omitempty"`
	ReceiveTimeout                int      `json:"receive_timeout,omitempty"`
	SendTimeout                   int      `json:"send_timeout,omitempty"`
	SquashThreshold               int      `json:"squash_threshold,omitempty"`
}

// https://<host>:<mPort>/services/licenser/messages
// Access licenser messages.
// Messages may range from helpful warnings about being close to violations, licenses
// expiring or more severe alerts regarding overages and exceeding license warning window.
type LicenseMessage struct {
	Messages []string `json:"messages,omitempty"`
}

// https://<host>:<mPort>/services/licenser/pools
// Access the licenser pools configuration.
// A pool logically partitions the daily volume entitlements of a stack. You can use a
// license pool to divide license privileges amongst multiple peers.
type LicensePool struct {
}

// https://<host>:<mPort>/services/licenser/peers
// Access license peer instances.
type LicensePeer struct {
	ActivePoolIds  []string `json:"active_pool_ids,omitempty"`
	Label          string   `json:"splunk-lm-license-manager-0,omitempty"`
	PoolIds        []string `json:"pool_ids,omitempty"`
	PoolSuggestion string   `json:"pool_suggestion,omitempty"`
	StackIds       []string `json:"stack_ids,omitempty"`
	WarningCount   string   `json:"warning_count,omitempty"`
}

// https://<host>:<mPort>/services/licenser/stacks
// Provides access to the license stack configuration.
// A license stack is comprised of one or more licenses of the same "type".
// The daily indexing quota of a license stack is additive, so a stack represents
// the aggregate entitlement for a collection of licenses.
type LicenseStack struct {
	CleActive        int    `json:"cle_active,omitempty"`
	IsUnlimited      bool   `json:"is_unlimited,omitempty"`
	Label            string `json:"label,omitempty"`
	MaxRetentionSize int    `json:"max_retention_size,omitempty"`
	MaxViolations    int    `json:"max_violations,omitempty"`
	Quota            int    `json:"quota,omitempty"`
	Type             string `json:"type,omitempty"`
	WindowPeriod     int    `json:"window_period,omitempty"`
}

// LicenseUsage
// https://<host>:<mPort>/services/licenser/usage
// Get current license usage stats from the last minute.
type LicenseUsage struct {
	PeerUsageBytes   int `json:"peers_usage_bytes,omitempty"`
	Quota            int `json:"quota,omitempty"`
	SlavesUsageBytes int `json:"slaves_usage_bytes,omitempty"`
}
