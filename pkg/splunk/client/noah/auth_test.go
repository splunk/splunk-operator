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
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHMACV3Authenticator(t *testing.T) {
	pass4SymmKey := []byte(t.Name())
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	random := append(make([]byte, hmacNonceBytes), []byte{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}...)
	authenticator, err := newHMACV3Authenticator(pass4SymmKey, now, bytes.NewReader(random))
	assert.NoError(t, err)
	clear(pass4SymmKey)

	request, err := http.NewRequest(http.MethodGet, "https://noah.test/tenant/noah/v1/peers", nil)
	assert.NoError(t, err)
	assert.NoError(t, authenticator.Authenticate(request, nil))

	assert.Equal(t, strings.Repeat("a", hmacNonceBytes), request.Header.Get(hmacNonceHeader))
	assert.Equal(t, "1700000000", request.Header.Get(hmacTimestampHeader))
	assert.Equal(t,
		"v3,wNo+n3P9VpXBYbhhJvwjhIRBTQgUZCIbLe5Dp7Bj21Oc0nqfc8ZFGnwDjTLt19UKlm9Ubs0DG6lf2OA3mi3WOQ==",
		request.Header.Get(hmacDigestHeader),
	)
	assert.Equal(t,
		"v3,@salt=AAECAwQFBgcICQoLDA0ODw==@iterCount=1000",
		request.Header.Get(hmacDigestKeyParamsHeader),
	)
}

func TestNewHMACV3AuthenticatorRejectsEmptyKey(t *testing.T) {
	_, err := NewHMACV3Authenticator(nil)
	assert.Error(t, err)
}

func TestNewHMACV3AuthenticatorValidatesDependencies(t *testing.T) {
	tests := []struct {
		name   string
		now    func() time.Time
		random io.Reader
	}{
		{name: "nil clock", random: bytes.NewReader(nil)},
		{name: "nil random source", now: time.Now},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newHMACV3Authenticator([]byte(t.Name()), test.now, test.random)
			assert.Error(t, err)
		})
	}
}

func TestHMACV3AuthenticatorReportsRandomSourceFailures(t *testing.T) {
	tests := []struct {
		name   string
		random io.Reader
		want   string
	}{
		{name: "nonce", random: iotest.ErrReader(assert.AnError), want: "generate nonce"},
		{
			name:   "salt",
			random: io.MultiReader(bytes.NewReader(make([]byte, hmacNonceBytes)), iotest.ErrReader(assert.AnError)),
			want:   "generate digest salt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator, err := newHMACV3Authenticator([]byte(t.Name()), time.Now, test.random)
			assert.NoError(t, err)
			request, err := http.NewRequest(http.MethodGet, "https://noah.test/tenant/noah/v1/peers", nil)
			assert.NoError(t, err)

			assert.ErrorContains(t, authenticator.Authenticate(request, nil), test.want)
		})
	}
}

func TestHMACV3AuthenticatorRejectsNilRequest(t *testing.T) {
	authenticator, err := NewHMACV3Authenticator([]byte(t.Name()))
	assert.NoError(t, err)
	assert.Error(t, authenticator.Authenticate(nil, nil))
}
