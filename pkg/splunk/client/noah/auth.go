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
	hmacV2NonceHeader     = "x-splunk-lm-nonce"
	hmacV2TimestampHeader = "x-splunk-lm-timestamp"
	hmacV2DigestHeader    = "x-splunk-digest"
	hmacV2NonceCharacters = "abcdefghijklmnopqrstuvwxyz1234567890"
)

type hmacV2Authenticator struct {
	generatedKey []byte
	now          func() time.Time
	random       io.Reader
}

// NewHMACV2Authenticator creates the request authenticator used by Noah's
// administrative API. It retains only the key derived from pass4SymmKey.
func NewHMACV2Authenticator(pass4SymmKey []byte) (Authenticator, error) {
	return newHMACV2Authenticator(pass4SymmKey, time.Now, rand.Reader)
}

func newHMACV2Authenticator(pass4SymmKey []byte, now func() time.Time, random io.Reader) (*hmacV2Authenticator, error) {
	if len(pass4SymmKey) == 0 {
		return nil, fmt.Errorf("pass4SymmKey is empty")
	}
	if now == nil {
		return nil, fmt.Errorf("clock is nil")
	}
	if random == nil {
		return nil, fmt.Errorf("random source is nil")
	}

	derivedKey := pbkdf2.Key(pass4SymmKey, nil, 100_000, 64, sha512.New)
	return &hmacV2Authenticator{
		generatedKey: []byte(base64.StdEncoding.EncodeToString(derivedKey)),
		now:          now,
		random:       random,
	}, nil
}

func (auth *hmacV2Authenticator) Authenticate(request *http.Request, body []byte) error {
	if request == nil {
		return fmt.Errorf("request is nil")
	}

	randomBytes := make([]byte, 32)
	if _, err := io.ReadFull(auth.random, randomBytes); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	nonce := make([]byte, len(randomBytes))
	for index, value := range randomBytes {
		nonce[index] = hmacV2NonceCharacters[int(value)%len(hmacV2NonceCharacters)]
	}

	timestamp := fmt.Sprintf("%d", auth.now().Unix())
	serialized := strings.Join([]string{
		string(nonce),
		timestamp,
		request.Method,
		request.URL.Path,
		strings.TrimSpace(string(body)),
	}, "\x00")
	digest := hmac.New(sha512.New, auth.generatedKey)
	_, _ = digest.Write([]byte(serialized))

	request.Header.Set(hmacV2NonceHeader, string(nonce))
	request.Header.Set(hmacV2TimestampHeader, timestamp)
	request.Header.Set(hmacV2DigestHeader, "v2,"+base64.StdEncoding.EncodeToString(digest.Sum(nil)))
	return nil
}
