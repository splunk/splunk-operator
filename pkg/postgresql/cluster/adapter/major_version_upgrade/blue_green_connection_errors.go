/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package majorupgradeadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// Transport failure cannot undo the previously verified durable LOGIN/HBA
// fence. Authentication and certificate failures are contradictory live state
// and must never be treated as an absent retired instance.
func retainedInstanceUnavailable(err error) bool {
	return allCutoverErrorLeaves(err, func(cause error) bool {
		switch failure := cause.(type) {
		case *pgconn.PgError:
			return failure.Code == "57P01" || failure.Code == "57P02" || failure.Code == "57P03"
		case *net.DNSError:
			return true
		}
		switch cause {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED,
			syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH,
			syscall.ENETDOWN, syscall.EPIPE, context.DeadlineExceeded,
			io.EOF, io.ErrUnexpectedEOF:
			return true
		}
		return false
	})
}

// pgx can join failures from multiple connection attempts. Every branch must
// prove the expected failure: finding one permitted error cannot conceal an
// authentication, TLS, or unknown failure in another branch.
func allCutoverErrorLeaves(err error, allowed func(error) bool) bool {
	if err == nil {
		return false
	}
	switch failure := err.(type) {
	case *net.OpError:
		if failure.Op == "remote error" || failure.Op == "local error" {
			return false
		}
	case tls.AlertError, *tls.AlertError, *tls.CertificateVerificationError,
		x509.UnknownAuthorityError, *x509.UnknownAuthorityError,
		x509.HostnameError, *x509.HostnameError,
		x509.CertificateInvalidError, *x509.CertificateInvalidError,
		tls.RecordHeaderError, *tls.RecordHeaderError:
		return false
	}
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := unwrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !allCutoverErrorLeaves(cause, allowed) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		if cause := unwrapped.Unwrap(); cause != nil {
			return allCutoverErrorLeaves(cause, allowed)
		}
	}
	return allowed(err)
}

func intentionalFenceDenial(err error) bool {
	return allCutoverErrorLeaves(err, func(cause error) bool {
		pgErr, ok := cause.(*pgconn.PgError)
		if !ok || pgErr.Code != "28000" {
			return false
		}
		message := strings.ToLower(pgErr.Message)
		return strings.Contains(message, "pg_hba.conf rejects connection") || strings.Contains(message, "not permitted to log in")
	})
}
