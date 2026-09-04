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

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/pbkdf2"
)

func TestHMACV2Authenticator(t *testing.T) {
	pass4SymmKey := []byte("unit-test-noah-key")
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	authenticator, err := newHMACV2Authenticator(pass4SymmKey, now, bytes.NewReader(make([]byte, 32)))
	assert.NoError(t, err)
	request, err := http.NewRequest(http.MethodGet, "https://noah.test/tenant/noah/v1/peers", nil)
	assert.NoError(t, err)
	assert.NoError(t, authenticator.Authenticate(request, nil))

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

	assert.Equal(t, nonce, request.Header.Get(hmacV2NonceHeader))
	assert.Equal(t, timestamp, request.Header.Get(hmacV2TimestampHeader))
	wantDigest := "v2," + base64.StdEncoding.EncodeToString(digest.Sum(nil))
	assert.Equal(t, wantDigest, request.Header.Get(hmacV2DigestHeader))
}

func TestNewHMACV2AuthenticatorRejectsEmptyKey(t *testing.T) {
	_, err := NewHMACV2Authenticator(nil)
	assert.Error(t, err)
}
