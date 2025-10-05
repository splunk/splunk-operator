// pkg/tls/defaults.go
package tls

import (
	"path/filepath"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// Data is the input model for templates/pretasks.tmpl.yaml.
// NOTE: Go renders with custom delimiters [[ ... ]] so Ansible/Jinja {{ ... }} stays intact.
type Data struct {
	// Core directories (resolved)
	SplunkHome   string // from SplunkHomeDefault
	CanonicalDir string // spec override or CanonicalTLSDir

	// Mounted sources (resolved from constants; mutator mounts them)
	SrcDir   string // TLSSrcMountDir
	TrustDir string // TrustSrcMountDir
	TrustKey string // key name inside the trust-bundle Secret (default "ca-bundle.crt")

	// Canonical destination file paths (under CanonicalDir)
	TLSCrt    string // CanonicalDir/TLSCrtName
	TLSKey    string // CanonicalDir/TLSKeyName
	CACrt     string // CanonicalDir/CACrtName
	ServerPEM string // CanonicalDir/ServerPEMName
	CABundle  string // CanonicalDir/TrustBundleName

	// KV encrypted key/bundle (optional)
	KVEnable       bool   // spec.KVEncryptedKey.Enabled
	KVBundlePath   string // CanonicalDir/<bundleFile or KVBundleDefaultName>
	KVPasswordFile string // set by mutator if a Secret is mounted (may be empty)

	// Optional serverName for [general]; the caller sets if desired
	ServerName string

    KVBundleAliasCRT string // CanonicalDir + "/splunk-bundle-pass.crt"
    KVBundleAliasPEM string // CanonicalDir + "/splunk-bundle-pass.pem"
}

// defaultsFor applies operator defaults using constants from constants.go.
func defaultsFor(spec *v4.TLSConfig) Data {
	// Trust-manager Secret default key (source). Destination file name is TrustBundleName.
	const defaultTrustSecretKey = "ca-bundle.crt"

	d := Data{
		SplunkHome: SplunkHomeDefault,
		SrcDir:     TLSSrcMountDir,
		TrustDir:   TrustSrcMountDir,
		TrustKey:   defaultTrustSecretKey,
	}

	// CanonicalDir (spec override)
	canon := spec.CanonicalDir
	if canon == "" {
		canon = CanonicalTLSDir
	}
	d.CanonicalDir = canon

	// Canonical file paths
	d.TLSCrt = filepath.Join(canon, TLSCrtName)
	d.TLSKey = filepath.Join(canon, TLSKeyName)
	d.CACrt = filepath.Join(canon, CACrtName)
	d.ServerPEM = filepath.Join(canon, ServerPEMName)
	d.CABundle = filepath.Join(canon, TrustBundleName)

	// Trust bundle Secret key override (source key), if provided
	if spec.TrustBundle != nil && spec.TrustBundle.Key != "" {
		d.TrustKey = spec.TrustBundle.Key
	}

	// KV encrypted key settings
	if spec.KVEncryptedKey != nil && spec.KVEncryptedKey.Enabled {
		d.KVEnable = true
		bundleFile := KVBundleDefaultName
		if bf := spec.KVEncryptedKey.BundleFile; bf != "" {
			bundleFile = bf
		}
		d.KVBundlePath = filepath.Join(canon, bundleFile)
		// d.KVPasswordFile is set by the mutator if you mount a password Secret at KVPassSrcMountDir.
	}

	// Always expose the two common alias names in the canonical dir
    d.KVBundleAliasCRT = d.CanonicalDir + "/splunk-bundle-pass.crt"
    d.KVBundleAliasPEM = d.CanonicalDir + "/splunk-bundle-pass.pem"

	// d.ServerName (and d.KVPasswordFile) are set by the caller (renderer/mutator) as needed.
	return d
}
