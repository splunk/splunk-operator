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
	"reflect"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// PostgresStatusChangedPredicate observes dependency readiness and deletion changes.
func PostgresStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch old := e.ObjectOld.(type) {
			case *platformApi.PostgresDatabase:
				updated, ok := e.ObjectNew.(*platformApi.PostgresDatabase)
				return ok && (!reflect.DeepEqual(old.Status, updated.Status) || !reflect.DeepEqual(old.DeletionTimestamp, updated.DeletionTimestamp))
			default:
				return false
			}
		},
	}
}
