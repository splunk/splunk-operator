package tls

// Canonical locations inside the Splunk container
const (
	SplunkHomeDefault = "/opt/splunk"
	CanonicalTLSDir   = "/opt/splunk/etc/auth" // default; overridable by spec.tls.canonicalDir
	//CanonicalTLSDir     = "/opt/splunk/etc/auth" // default; overridable by spec.tls.canonicalDir

	// Source mounts
	TLSSrcMountDir    = "/mnt/certs"  // Secret or CSI
	TrustSrcMountDir  = "/mnt/trust"  // optional trust bundle Secret
	KVPassSrcMountDir = "/mnt/kvpass" // optional secret for kv password

	// PRE-TASKS mount
	PreTasksMountDir = "/mnt/pre"
	PreTasksFilename = "tls.yml"                 // combined Go-templated playbook
	PreTasksFileURI  = "file:///mnt/pre/tls.yml" // SPLUNK_ANSIBLE_PRE_TASKS value

	// Canonical filenames under CanonicalTLSDir
	TLSCrtName          = "tls.crt"
	TLSKeyName          = "tls.key"
	CACrtName           = "ca.crt"
	ServerPEMName       = "server.pem"
	TrustBundleName     = "trust-bundle.crt" // where we copy an optional trust bundle
	KVBundleDefaultName = "kvstore.pem"      // if kvEncryptedKey.enabled

	// K8s resource names (volume/configmap keys)
	PreTasksCMKey = "tls.yml" // key in ConfigMap data

	// Pod template annotations (visible diff even with OnDelete)
	AnnTLSChecksum      = "enterprise.splunk.com/tls-checksum"      // sha256 of observed TLS inputs
	AnnPreTasksChecksum = "enterprise.splunk.com/pretasks-checksum" // sha256 of rendered pretasks

	// Env
	EnvPreTasks  = "SPLUNK_ANSIBLE_PRE_TASKS"
	EnvStartArgs = "SPLUNK_START_ARGS"
)

// Small helper for int32 pointers
func Int32Ptr(v int32) *int32 { return &v }
func BoolPtr(b bool) *bool    { return &b }
