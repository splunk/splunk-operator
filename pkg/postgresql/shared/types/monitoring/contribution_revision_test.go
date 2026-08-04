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

package monitoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContributionRevision(t *testing.T) {
	selectors := []QuerySelector{
		{ConfigMapName: "one", ConfigMapKey: "queries.yaml"},
		{ConfigMapName: "two", ConfigMapKey: "queries.yaml"},
	}
	revision := ContributionRevision("orders", true, selectors)

	assert.Equal(t, revision, ContributionRevision("orders", true, selectors))
	assert.NotEqual(t, revision, ContributionRevision("orders", false, selectors))
	assert.NotEqual(t, revision, ContributionRevision("billing", true, selectors))
	assert.NotEqual(t, revision, ContributionRevision("orders", true, []QuerySelector{selectors[1], selectors[0]}),
		"selector order is part of collision precedence and must affect the revision")
}
