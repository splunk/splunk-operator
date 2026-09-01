// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

func TestHMACV2Authenticator(t *testing.T) {
	pass4SymmKey := []byte("unit-test-noah-key")
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	authenticator, err := newHMACV2Authenticator(pass4SymmKey, now, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatalf("newHMACV2Authenticator() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://noah.test/tenant/noah/v1/peers", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	if err := authenticator.Authenticate(request, nil); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	nonce := strings.Repeat("a", 32)
	timestamp := "1700000000"
	serialized := strings.Join([]string{
		nonce,
		timestamp,
		http.MethodGet,
		"/tenant/noah/v1/peers",
		"",
	}, "\x00")
	derivedKey := pbkdf2.Key(pass4SymmKey, nil, 100_000, 64, sha512.New)
	digest := hmac.New(sha512.New, []byte(base64.StdEncoding.EncodeToString(derivedKey)))
	_, _ = digest.Write([]byte(serialized))

	if got := request.Header.Get(hmacV2NonceHeader); got != nonce {
		t.Errorf("nonce header = %q, want %q", got, nonce)
	}
	if got := request.Header.Get(hmacV2TimestampHeader); got != timestamp {
		t.Errorf("timestamp header = %q, want %q", got, timestamp)
	}
	wantDigest := "v2," + base64.StdEncoding.EncodeToString(digest.Sum(nil))
	if got := request.Header.Get(hmacV2DigestHeader); got != wantDigest {
		t.Errorf("digest header = %q, want %q", got, wantDigest)
	}
}

func TestNewHMACV2AuthenticatorRejectsEmptyKey(t *testing.T) {
	if _, err := NewHMACV2Authenticator(nil); err == nil {
		t.Fatal("NewHMACV2Authenticator() error = nil, want empty-key error")
	}
}
