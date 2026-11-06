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

package majorversionupgradetypes

import (
	"encoding/json/v2"
	"reflect"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Missing a newly introduced timestamp must fail rather than silently bypassing
// write-once validation or commit authorization.
func TestMilestoneRegistryCoversEveryStatusTimestamp(t *testing.T) {
	fields := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[platformv1alpha1.BlueGreenSwitchoverStatus]()) {
		if field.Type == reflect.TypeFor[*metav1.Time]() {
			fields[field.Name] = false
		}
	}
	for _, milestone := range SwitchoverMilestones() {
		covered, exists := fields[milestone.Field]
		if !exists || covered {
			t.Fatalf("duplicate or unknown timestamp %s", milestone.Field)
		}
		state := new(platformv1alpha1.BlueGreenSwitchoverStatus)
		now := metav1.NewTime(time.Unix(100, 0))
		milestone.Record(state, now)
		actual := reflect.ValueOf(state).Elem().FieldByName(milestone.Field).Interface().(*metav1.Time)
		if !actual.Equal(&now) || !milestone.Time(state).Equal(&now) {
			t.Fatalf("wrong timestamp accessor for %s", milestone.Field)
		}
		fields[milestone.Field] = true
	}
	for field, covered := range fields {
		if !covered {
			t.Errorf("timestamp %s has no lifecycle definition", field)
		}
	}
}

func TestWindowReplacementPreservesAttemptStateAndWireFormat(t *testing.T) {
	now := metav1.NewTime(time.Unix(100, 0))
	before := &platformv1alpha1.BlueGreenSwitchoverStatus{
		Sequence: 1, StartedAt: now.DeepCopy(), Failure: new("previous window"),
		OriginalHBA: new([]string{"blue rule"}), GreenOriginalHBA: new([]string{"green rule"}),
		CandidateGuardsRemovedAt: now.DeepCopy(), ControlIdentity: &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control"},
	}
	after := before.DeepCopy()
	after.BlueGreenSwitchoverWindow = platformv1alpha1.BlueGreenSwitchoverWindow{Sequence: 2, RetryRequestedAt: now.DeepCopy()}
	if after.StartedAt != nil || after.Failure != nil || after.ControlIdentity.RoleName != "control" || after.CandidateGuardsRemovedAt == nil {
		t.Fatal("window replacement crossed lifetimes")
	}
	(*after.OriginalHBA)[0] = "changed"
	if (*before.OriginalHBA)[0] != "blue rule" {
		t.Fatal("attempt baseline aliases the prior status")
	}
	data, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["startedAt"]; !ok {
		t.Fatal("existing window JSON field moved")
	}
	if _, ok := fields["controlIdentity"]; !ok {
		t.Fatal("existing access JSON field moved")
	}
	if _, ok := fields["BlueGreenCutoverAccessStatus"]; ok {
		t.Fatal("Go lifetime group leaked into JSON")
	}
}
