/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRules(t *testing.T) {
	t.Run("nil slice returns nil", func(t *testing.T) {
		assert.NoError(t, ValidateRules(nil))
	})

	t.Run("empty slice returns nil", func(t *testing.T) {
		assert.NoError(t, ValidateRules([]string{}))
	})

	t.Run("all valid rules returns nil", func(t *testing.T) {
		rules := []string{
			"local all all trust",
			"host all all 0.0.0.0/0 scram-sha-256",
			"hostssl all all 192.168.1.0/24 md5",
		}
		assert.NoError(t, ValidateRules(rules))
	})

	t.Run("mixed valid and invalid returns error with correct indices", func(t *testing.T) {
		rules := []string{
			"host all all 0.0.0.0/0 scram-sha-256",
			"hostx all all 0.0.0.0/0 md5",
			"host all all 0.0.0.0/0 md5",
		}
		err := ValidateRules(rules)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rule 2:")
		assert.NotContains(t, err.Error(), "rule 1:")
		assert.NotContains(t, err.Error(), "rule 3:")
	})

	t.Run("multiple errors in different rules", func(t *testing.T) {
		rules := []string{
			"hostx all all 0.0.0.0/0 md5",
			"host all all 192.168.0.0/33 bogus",
		}
		err := ValidateRules(rules)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rule 1:")
		assert.Contains(t, err.Error(), "rule 2:")
	})
}

func TestValidateRule(t *testing.T) {
	// === Valid rules ===

	validRules := []struct {
		name string
		rule string
	}{
		{"local basic", "local all all trust"},
		{"local with peer", "local postgres postgres peer"},
		{"host CIDR IPv4", "host all all 0.0.0.0/0 scram-sha-256"},
		{"hostssl CIDR", "hostssl all all 192.168.1.0/24 md5"},
		{"hostnossl reject", "hostnossl all all 0.0.0.0/0 reject"},
		{"hostgssenc", "hostgssenc all all 0.0.0.0/0 gss"},
		{"hostnogssenc", "hostnogssenc all all 0.0.0.0/0 scram-sha-256"},
		{"host replication", "host replication all 10.0.0.0/8 password"},
		{"host samehost", "host all all samehost trust"},
		{"host samenet", "host all all samenet trust"},
		{"host address all", "host all all all scram-sha-256"},
		{"host domain suffix", "host all all .example.com cert"},
		{"host IPv6", "host all all ::1/128 scram-sha-256"},
		{"host IPv6 all", "host all all ::0/0 md5"},
		{"host IP+netmask", "host all all 192.168.1.1 255.255.255.0 md5"},
		{"host IP+netmask /8", "host all all 10.0.0.0 255.0.0.0 md5"},
		{"inline comment", "host all all 0.0.0.0/0 md5 # office access"},
		{"inline comment with spaces", "host all all 0.0.0.0/0 md5   # allow all"},
		{"full-line comment", "# this is a comment"},
		{"comment-only with spaces", "  # indented comment"},
		{"host auth options", "host all all 0.0.0.0/0 ldap ldapserver=ldap.example.com ldapport=389"},
		{"host quoted option", "host all all 0.0.0.0/0 ident map=omicron"},
		{"host quoted value", `host all all 0.0.0.0/0 ldap ldapprefix="cn="`},
		{"comma-separated db", "host db1,db2 all 0.0.0.0/0 md5"},
		{"comma-separated user", "host all user1,user2 0.0.0.0/0 md5"},
		{"host hostname", "host all all myhost.example.com md5"},
		{"host with sspi", "host all all 0.0.0.0/0 sspi"},
		{"host with ident", "host all all 0.0.0.0/0 ident"},
		{"host with pam", "host all all 0.0.0.0/0 pam"},
		{"host with radius", "host all all 0.0.0.0/0 radius"},
		{"empty string", ""},
		{"whitespace only", "   "},
	}

	for _, tc := range validRules {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			errs := validateRule(tc.rule)
			assert.Empty(t, errs, "expected no errors for rule %q, got: %v", tc.rule, errs)
		})
	}

	// === Layer 0: connection type errors ===

	t.Run("layer0/unknown connection type", func(t *testing.T) {
		errs := validateRule("hostx all all 0.0.0.0/0 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], `unknown connection type "hostx"`)
	})

	t.Run("layer0/uppercase connection type", func(t *testing.T) {
		errs := validateRule("HOST all all 0.0.0.0/0 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], `unknown connection type "HOST"`)
	})

	// === Layer 1: field count errors ===

	t.Run("layer1/host missing method", func(t *testing.T) {
		errs := validateRule("host all all 0.0.0.0/0")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "too few fields")
	})

	t.Run("layer1/host only three fields", func(t *testing.T) {
		errs := validateRule("host all all")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "too few fields")
	})

	t.Run("layer1/local missing user and method", func(t *testing.T) {
		errs := validateRule("local all")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "too few fields")
	})

	t.Run("layer1/local missing method", func(t *testing.T) {
		errs := validateRule("local all all")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "too few fields")
	})

	// === Layer 2: auth method errors ===

	t.Run("layer2/unknown auth method", func(t *testing.T) {
		errs := validateRule("host all all 0.0.0.0/0 bogus")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], `unknown auth method "bogus"`)
	})

	t.Run("layer2/typo scram-sha256", func(t *testing.T) {
		errs := validateRule("host all all 0.0.0.0/0 scram-sha256")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], `unknown auth method "scram-sha256"`)
	})

	t.Run("layer2/local unknown method", func(t *testing.T) {
		errs := validateRule("local all all unknown")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], `unknown auth method "unknown"`)
	})

	// === Layer 3: address errors ===

	t.Run("layer3/invalid CIDR mask too large", func(t *testing.T) {
		errs := validateRule("host all all 192.168.0.0/33 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "invalid CIDR")
	})

	t.Run("layer3/invalid IP in CIDR", func(t *testing.T) {
		errs := validateRule("host all all 256.1.1.1/24 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "invalid CIDR")
	})

	t.Run("layer3/garbage address", func(t *testing.T) {
		errs := validateRule("host all all not@valid md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "invalid address")
	})

	// === Layer 3: netmask errors ===

	t.Run("layer3/non-contiguous netmask", func(t *testing.T) {
		errs := validateRule("host all all 10.0.0.1 255.0.255.0 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "not a contiguous subnet mask")
	})

	t.Run("layer3/invalid IP in netmask pair", func(t *testing.T) {
		errs := validateRule("host all all 999.0.0.1 255.255.255.0 md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "invalid IP address")
	})

	t.Run("layer3/garbage netmask", func(t *testing.T) {
		errs := validateRule("host all all 10.0.0.1 notamask md5")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "invalid netmask")
	})

	// === Multiple errors in one rule ===

	t.Run("multiple/bad method and bad address", func(t *testing.T) {
		errs := validateRule("host all all 192.168.0.0/33 bogus")
		assert.Len(t, errs, 2)
	})
}

func TestTokenize(t *testing.T) {
	t.Run("simple fields", func(t *testing.T) {
		tokens := tokenize("host all all 0.0.0.0/0 md5")
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "md5"}, tokens)
	})

	t.Run("multiple spaces", func(t *testing.T) {
		tokens := tokenize("host  all   all    0.0.0.0/0   md5")
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "md5"}, tokens)
	})

	t.Run("quoted string preserved", func(t *testing.T) {
		tokens := tokenize(`host all all 0.0.0.0/0 ldap ldapprefix="cn="`)
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "ldap", `ldapprefix="cn="`}, tokens)
	})

	t.Run("auth option with equals", func(t *testing.T) {
		tokens := tokenize("host all all 0.0.0.0/0 ident map=omicron")
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "ident", "map=omicron"}, tokens)
	})

	t.Run("empty string", func(t *testing.T) {
		tokens := tokenize("")
		assert.Empty(t, tokens)
	})

	t.Run("inline comment stripped", func(t *testing.T) {
		tokens := tokenize("host all all 0.0.0.0/0 md5 # office access")
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "md5"}, tokens)
	})

	t.Run("full-line comment", func(t *testing.T) {
		tokens := tokenize("# this is a comment")
		assert.Empty(t, tokens)
	})

	t.Run("hash inside quotes not treated as comment", func(t *testing.T) {
		tokens := tokenize(`host all all 0.0.0.0/0 ldap ldapprefix="cn=#test"`)
		assert.Equal(t, []string{"host", "all", "all", "0.0.0.0/0", "ldap", `ldapprefix="cn=#test"`}, tokens)
	})
}

func TestStripComment(t *testing.T) {
	t.Run("no comment", func(t *testing.T) {
		assert.Equal(t, "host all all 0.0.0.0/0 md5", stripComment("host all all 0.0.0.0/0 md5"))
	})

	t.Run("inline comment", func(t *testing.T) {
		assert.Equal(t, "host all all 0.0.0.0/0 md5 ", stripComment("host all all 0.0.0.0/0 md5 # comment"))
	})

	t.Run("full-line comment", func(t *testing.T) {
		assert.Equal(t, "", stripComment("# full line comment"))
	})

	t.Run("hash inside quotes preserved", func(t *testing.T) {
		assert.Equal(t, `host all all 0.0.0.0/0 ldap ldapprefix="cn=#x"`, stripComment(`host all all 0.0.0.0/0 ldap ldapprefix="cn=#x"`))
	})

	t.Run("hash after closing quote", func(t *testing.T) {
		assert.Equal(t, `host all all 0.0.0.0/0 ldap ldapprefix="cn" `, stripComment(`host all all 0.0.0.0/0 ldap ldapprefix="cn" # comment`))
	})
}

func TestValidateIPNetmask(t *testing.T) {
	t.Run("valid IPv4", func(t *testing.T) {
		assert.Empty(t, validateIPNetmask("192.168.1.0", "255.255.255.0"))
	})

	t.Run("valid /8", func(t *testing.T) {
		assert.Empty(t, validateIPNetmask("10.0.0.0", "255.0.0.0"))
	})

	t.Run("invalid IP", func(t *testing.T) {
		result := validateIPNetmask("999.0.0.1", "255.255.255.0")
		assert.Contains(t, result, "invalid IP address")
	})

	t.Run("invalid mask not an IP", func(t *testing.T) {
		result := validateIPNetmask("10.0.0.1", "notamask")
		assert.Contains(t, result, "invalid netmask")
	})

	t.Run("non-contiguous mask", func(t *testing.T) {
		result := validateIPNetmask("10.0.0.1", "255.0.255.0")
		assert.Contains(t, result, "not a contiguous subnet mask")
	})
}

func TestValidateAddress(t *testing.T) {
	validAddresses := []string{
		"0.0.0.0/0",
		"192.168.1.0/24",
		"10.0.0.0/8",
		"::1/128",
		"::0/0",
		"all",
		"samehost",
		"samenet",
		".example.com",
		".sub.domain.com",
		"192.168.1.1",
		"myhost.example.com",
		"my-host",
		"localhost",
	}

	for _, addr := range validAddresses {
		t.Run("valid/"+addr, func(t *testing.T) {
			assert.Empty(t, validateAddress(addr))
		})
	}

	invalidAddresses := []struct {
		name    string
		address string
		errMsg  string
	}{
		{"CIDR mask too large", "192.168.0.0/33", "invalid CIDR"},
		{"invalid IP in CIDR", "256.1.1.1/24", "invalid CIDR"},
		{"bad CIDR format", "999.999.999.999/32", "invalid CIDR"},
		{"special chars", "host@name", "invalid address"},
		{"spaces in addr", "my host", "invalid address"},
	}

	for _, tc := range invalidAddresses {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			result := validateAddress(tc.address)
			assert.Contains(t, result, tc.errMsg)
		})
	}
}
