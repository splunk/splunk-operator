// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package common

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestPostgresStatusChangedPredicate(t *testing.T) {
	ready := "Ready"
	pending := "Pending"
	now := metav1.Now()
	tests := []struct {
		name string
		old  client.Object
		new  client.Object
		want bool
	}{
		{
			name: "cluster phase change ignored",
			old:  &platformApi.PostgresCluster{Status: platformApi.PostgresClusterStatus{Phase: &pending}},
			new:  &platformApi.PostgresCluster{Status: platformApi.PostgresClusterStatus{Phase: &ready}},
			want: false,
		},
		{
			name: "database phase change",
			old:  &platformApi.PostgresDatabase{Status: platformApi.PostgresDatabaseStatus{Phase: &pending}},
			new:  &platformApi.PostgresDatabase{Status: platformApi.PostgresDatabaseStatus{Phase: &ready}},
			want: true,
		},
		{
			name: "unchanged postgres status",
			old:  &platformApi.PostgresCluster{Status: platformApi.PostgresClusterStatus{Phase: &ready}},
			new:  &platformApi.PostgresCluster{Status: platformApi.PostgresClusterStatus{Phase: &ready}},
			want: false,
		},
		{
			name: "cluster deletion ignored",
			old:  &platformApi.PostgresCluster{Status: platformApi.PostgresClusterStatus{Phase: &ready}},
			new:  &platformApi.PostgresCluster{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}, Status: platformApi.PostgresClusterStatus{Phase: &ready}},
			want: false,
		},
		{
			name: "database starts deleting",
			old:  &platformApi.PostgresDatabase{Status: platformApi.PostgresDatabaseStatus{Phase: &ready}},
			new:  &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}, Status: platformApi.PostgresDatabaseStatus{Phase: &ready}},
			want: true,
		},
		{
			name: "KVService status update",
			old:  &enterpriseApi.KVService{},
			new:  &enterpriseApi.KVService{},
			want: false,
		},
	}

	p := PostgresStatusChangedPredicate()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.Update(event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}))
		})
	}
}
