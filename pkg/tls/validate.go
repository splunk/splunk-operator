// pkg/splunk/enterprise/tls/validate.go

package tls

import (
	"fmt"
	v4 "github.com/splunk/splunk-operator/api/v4"
)

func ValidateTLSSpec(t *v4.TLSConfig) error {
	if t == nil {
		return nil
	}
	if t.KVEncryptedKey != nil && t.KVEncryptedKey.Enabled {
		// nothing hard to validate; optional SecretKeySelector is fine
		if t.CanonicalDir == "" {
			return fmt.Errorf("tls.canonicalDir is required when kvEncryptedKey.enabled=true")
		}
	}
	return nil
}
