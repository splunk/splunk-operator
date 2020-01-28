// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resources

import (
	"testing"
)

func resourceQuantityTester(t *testing.T, str string, want int64) {
	q, err := ParseResourceQuantity(str, "")
	if err != nil {
		t.Errorf("ParseResourceQuantity(\"%s\") error: %v", str, err)
	}

	got, success := q.AsInt64()
	if !success {
		t.Errorf("ParseResourceQuantity(\"%s\") returned false", str)
	}
	if got != want {
		t.Errorf("ParseResourceQuantity(\"%s\") = %d; want %d", str, got, want)
	}
}

func TestParseResourceQuantity(t *testing.T) {
	resourceQuantityTester(t, "1Gi", 1073741824)
}
