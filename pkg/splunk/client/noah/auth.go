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
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	hmacNonceHeader           = "x-splunk-lm-nonce"
	hmacTimestampHeader       = "x-splunk-lm-timestamp"
	hmacDigestHeader          = "x-splunk-digest"
	hmacDigestKeyParamsHeader = "x-splunk-digest-key-params"
	hmacNonceCharacters       = "abcdefghijklmnopqrstuvwxyz1234567890"
	hmacNonceBytes            = 32
	hmacV3SaltBytes           = 16
	hmacV3Iterations          = 1000
	hmacKeyBytes              = 64
)

type hmacV3Authenticator struct {
	pass4SymmKey []byte
	now          func() time.Time
	random       io.Reader
}

// NewHMACV3Authenticator creates the v3 request authenticator used by Noah's
// administrative API.
func NewHMACV3Authenticator(pass4SymmKey []byte) (Authenticator, error) {
	return newHMACV3Authenticator(pass4SymmKey, time.Now, rand.Reader)
}

func newHMACV3Authenticator(pass4SymmKey []byte, now func() time.Time, random io.Reader) (*hmacV3Authenticator, error) {
	if len(pass4SymmKey) == 0 {
		return nil, fmt.Errorf("pass4SymmKey is empty")
	}
	if now == nil {
		return nil, fmt.Errorf("clock is nil")
	}
	if random == nil {
		return nil, fmt.Errorf("random source is nil")
	}

	return &hmacV3Authenticator{
		pass4SymmKey: bytes.Clone(pass4SymmKey),
		now:          now,
		random:       random,
	}, nil
}

func (auth *hmacV3Authenticator) Authenticate(request *http.Request, body []byte) error {
	if request == nil {
		return fmt.Errorf("request is nil")
	}

	randomBytes := make([]byte, hmacNonceBytes)
	if _, err := io.ReadFull(auth.random, randomBytes); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	nonce := make([]byte, len(randomBytes))
	for index, value := range randomBytes {
		nonce[index] = hmacNonceCharacters[int(value)%len(hmacNonceCharacters)]
	}

	salt := make([]byte, hmacV3SaltBytes)
	if _, err := io.ReadFull(auth.random, salt); err != nil {
		return fmt.Errorf("generate digest salt: %w", err)
	}

	timestamp := fmt.Sprintf("%d", auth.now().Unix())
	serialized := strings.Join([]string{
		string(nonce),
		timestamp,
		request.Method,
		request.URL.Path,
		strings.TrimSpace(string(body)),
	}, "\x00")
	derivedKey := pbkdf2.Key(auth.pass4SymmKey, salt, hmacV3Iterations, hmacKeyBytes, sha512.New)
	digest := hmac.New(sha512.New, derivedKey)
	_, _ = digest.Write([]byte(serialized))

	request.Header.Set(hmacNonceHeader, string(nonce))
	request.Header.Set(hmacTimestampHeader, timestamp)
	request.Header.Set(hmacDigestHeader, "v3,"+base64.StdEncoding.EncodeToString(digest.Sum(nil)))
	request.Header.Set(hmacDigestKeyParamsHeader, fmt.Sprintf(
		"v3,@salt=%s@iterCount=%d",
		base64.StdEncoding.EncodeToString(salt),
		hmacV3Iterations,
	))
	return nil
}
