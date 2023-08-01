package license

// https://<host>:<mPort>/services/licenser/groups
// Provides access to the configuration of licenser groups.
// A licenser group contains one or more licenser stacks that can operate concurrently.
// Only one licenser group is active at any given time.
type LicenseGroup struct {
}

// https://<host>:<mPort>/services/licenser/licenses
// Provides access to the licenses for this Splunk Enterprise instance.
// A license enables various features for a Splunk instance, including but not limited
// to indexing quota, auth, search, forwarding.
type License struct {
}

// https://<host>:<mPort>/services/licenser/localpeer
// Get license state information for the Splunk instance.
type LicenseLocalPeer struct {
}

// https://<host>:<mPort>/services/licenser/messages
// Access licenser messages.
// Messages may range from helpful warnings about being close to violations, licenses
// expiring or more severe alerts regarding overages and exceeding license warning window.
type LicenseMessage struct {
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
}

// https://<host>:<mPort>/services/licenser/stacks
// Provides access to the license stack configuration.
// A license stack is comprised of one or more licenses of the same "type".
// The daily indexing quota of a license stack is additive, so a stack represents
// the aggregate entitlement for a collection of licenses.
type LicenseStack struct {
}

// LicenseUsage
// https://<host>:<mPort>/services/licenser/usage
// Get current license usage stats from the last minute.
type LicenseUsage struct {
}
