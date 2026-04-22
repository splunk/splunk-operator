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
	"fmt"
	"net"
	"regexp"
	"strings"
)

var hbaConnectionTypes = map[string]bool{
	"local":        true,
	"host":         true,
	"hostssl":      true,
	"hostnossl":    true,
	"hostgssenc":   true,
	"hostnogssenc": true,
}

var hbaAuthMethods = map[string]bool{
	"trust":         true,
	"reject":        true,
	"scram-sha-256": true,
	"md5":           true,
	"password":      true,
	"gss":           true,
	"sspi":          true,
	"ident":         true,
	"peer":          true,
	"pam":           true,
	"ldap":          true,
	"radius":        true,
	"cert":          true,
	"oauth":         true,
}

var hbaSpecialAddresses = map[string]bool{
	"all":      true,
	"samehost": true,
	"samenet":  true,
}

// tokenPattern splits on whitespace while keeping double-quoted strings intact.
var hbaTokenPattern = regexp.MustCompile(`(?:"+.*?"+|\S)+`)

// hbaLabelPattern matches a valid DNS label sequence (hostname or domain suffix).
var hbaLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)

// RuleError describes a validation error for a single pg_hba.conf rule.
type RuleError struct {
	Index   int
	Message string
}

// ValidateRules validates a slice of pg_hba.conf rule strings.
func ValidateRules(rules []string) []RuleError {
	var errs []RuleError
	for i, rule := range rules {
		for _, msg := range validateRule(rule) {
			errs = append(errs, RuleError{Index: i, Message: msg})
		}
	}
	return errs
}

// validateRule parses and validates a single pg_hba rule.
// Returns a list of validation errors (empty means valid).
func validateRule(rule string) []string {
	trimmed := strings.TrimSpace(rule)
	if trimmed == "" {
		return nil
	}

	tokens := tokenize(trimmed)
	if len(tokens) == 0 {
		return nil
	}

	var errs []string

	// Layer 0: connection type
	connType := tokens[0]
	if !hbaConnectionTypes[connType] {
		return []string{fmt.Sprintf("unknown connection type %q", connType)}
	}

	isLocal := connType == "local"
	minFields := 5 // TYPE DATABASE USER ADDRESS METHOD
	if isLocal {
		minFields = 4 // local DATABASE USER METHOD
	}
	if len(tokens) < minFields {
		return []string{fmt.Sprintf("too few fields: expected at least %d (%s DATABASE USER %sMETHOD), got %d",
			minFields, connType, map[bool]string{true: "", false: "ADDRESS "}[isLocal], len(tokens))}
	}

	methodIdx := 3 // local: tokens[3]
	if !isLocal {
		if len(tokens) > 5 && net.ParseIP(tokens[4]) != nil {
			methodIdx = 5
		} else {
			methodIdx = 4
		}
	}
	if methodIdx >= len(tokens) {
		return []string{fmt.Sprintf("too few fields: missing auth method")}
	}
	method := tokens[methodIdx]
	if !hbaAuthMethods[method] {
		errs = append(errs, fmt.Sprintf("unknown auth method %q", method))
	}

	if !isLocal {
		address := tokens[3]
		if methodIdx == 5 {
			if addrErr := validateIPNetmask(tokens[3], tokens[4]); addrErr != "" {
				errs = append(errs, addrErr)
			}
		} else {
			if addrErr := validateAddress(address); addrErr != "" {
				errs = append(errs, addrErr)
			}
		}
	}

	return errs
}

// stripComment removes pg_hba.conf comments: a # outside double quotes starts
// a comment that runs to the end of the line.
func stripComment(line string) string {
	inQuotes := false
	for i, ch := range line {
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case '#':
			if !inQuotes {
				return line[:i]
			}
		}
	}
	return line
}

// tokenize splits a rule string on whitespace, keeping double-quoted strings intact.
// Comments (# to end of line, outside quotes) are stripped first.
func tokenize(line string) []string {
	stripped := stripComment(line)
	matches := hbaTokenPattern.FindAllString(stripped, -1)
	var tokens []string
	for _, m := range matches {
		if s := strings.TrimSpace(m); s != "" {
			tokens = append(tokens, s)
		}
	}
	return tokens
}

// validateAddress validates the address field for host* connection types.
func validateAddress(address string) string {
	if hbaSpecialAddresses[address] {
		return ""
	}

	// Domain suffix match: .example.com
	if strings.HasPrefix(address, ".") && len(address) > 1 {
		suffix := address[1:]
		if hbaLabelPattern.MatchString(suffix) {
			return ""
		}
		return fmt.Sprintf("invalid domain suffix %q", address)
	}

	// CIDR notation
	if strings.Contains(address, "/") {
		if _, _, err := net.ParseCIDR(address); err != nil {
			return fmt.Sprintf("invalid CIDR address %q: %v", address, err)
		}
		return ""
	}

	// IP address without CIDR (used with separate netmask field)
	if ip := net.ParseIP(address); ip != nil {
		return ""
	}

	// Hostname
	if hbaLabelPattern.MatchString(address) {
		return ""
	}

	return fmt.Sprintf("invalid address %q: expected CIDR, IP, hostname, or special keyword (all, samehost, samenet)", address)
}

// validateIPNetmask validates the IP + netmask form (two separate fields).
func validateIPNetmask(ip, mask string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Sprintf("invalid IP address %q in IP/netmask pair", ip)
	}

	parsedMask := net.ParseIP(mask)
	if parsedMask == nil {
		return fmt.Sprintf("invalid netmask %q: not a valid IP address", mask)
	}

	// Verify the mask is a valid contiguous subnet mask.
	// Convert to 4 or 16 bytes depending on IPv4/IPv6.
	var maskBytes net.IPMask
	if v4 := parsedMask.To4(); v4 != nil {
		maskBytes = net.IPMask(v4)
	} else {
		maskBytes = net.IPMask(parsedMask.To16())
	}

	// net.IPMask.Size() returns (ones, bits); ones == 0 && bits == 0 means invalid mask
	ones, bits := maskBytes.Size()
	if ones == 0 && bits == 0 {
		return fmt.Sprintf("invalid netmask %q: not a contiguous subnet mask", mask)
	}

	// IP and mask must be the same address family
	ipIs4 := parsedIP.To4() != nil
	maskIs4 := parsedMask.To4() != nil
	if ipIs4 != maskIs4 {
		return fmt.Sprintf("IP %q and netmask %q are not the same address family", ip, mask)
	}

	return ""
}
