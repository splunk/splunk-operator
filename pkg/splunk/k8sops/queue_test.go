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

package k8sops

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetQueue(t *testing.T) {
	ctx := context.Background()
	queue := enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "queue",
			Namespace: "test",
		},
	}
	namespacedName := types.NamespacedName{Name: queue.GetName(), Namespace: queue.GetNamespace()}
	c := spltest.NewMockClient()
	_, err := GetQueue(ctx, c, &queue, namespacedName)
	require.Error(t, err)

	c.AddObject(&queue)
	object, err := GetQueue(ctx, c, &queue, namespacedName)
	require.NoError(t, err)
	assert.Equal(t, queue.GetName(), object.GetName())
}
