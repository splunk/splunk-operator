// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
package helpers

import (
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricSample(t *testing.T) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(`# HELP example_value Example value
# TYPE example_value gauge
example_value{database="appdb",state="ready"} 7
`))
	require.NoError(t, err)

	value, found, err := MetricSample(families, "example_value", map[string]string{
		"database": "appdb",
		"state":    "ready",
	})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, float64(7), value)

	_, found, err = MetricSample(families, "example_value", map[string]string{"database": "appdb"})
	require.NoError(t, err)
	assert.False(t, found, "a subset must not satisfy exact label matching")

	_, found, err = MetricSample(families, "example_value", map[string]string{"database": "otherdb"})
	require.NoError(t, err)
	assert.False(t, found)
}
