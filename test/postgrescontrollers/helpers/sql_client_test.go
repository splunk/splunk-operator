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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConnectivityJobResult(t *testing.T) {
	for _, tt := range []struct {
		name             string
		condition        batchv1.JobCondition
		complete, failed bool
	}{
		{"running", batchv1.JobCondition{}, false, false},
		{"complete", batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}, true, false},
		{"failed", batchv1.JobCondition{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}, false, true},
		{"false failure condition", batchv1.JobCondition{Type: batchv1.JobFailed, Status: corev1.ConditionFalse}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			complete, err := connectivityJobResult(&batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{tt.condition}}})
			if complete != tt.complete || (err != nil) != tt.failed {
				t.Fatalf("got complete=%t error=%v", complete, err)
			}
		})
	}
}

func TestSQLJobOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/sql-test/pods/sql-client/log" || r.URL.Query().Get("container") != "psql" {
			t.Errorf("unexpected log request: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, "  appdb|appdb_rw\n")
	}))
	defer server.Close()
	podClient, err := coreclient.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "sql-job", Namespace: "sql-test", UID: types.UID("job-uid")}}
	for _, tt := range []struct {
		name      string
		owner     types.UID
		phase     corev1.PodPhase
		wantError bool
	}{
		{"completed owner", job.UID, corev1.PodSucceeded, false},
		{"other owner", types.UID("another-job"), corev1.PodSucceeded, true},
		{"not completed", job.UID, corev1.PodPending, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sql-client", Namespace: job.Namespace,
					Labels:          map[string]string{batchv1.ControllerUidLabel: string(job.UID)},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: tt.owner, Controller: ptr.To(true)}},
				},
				Status: corev1.PodStatus{Phase: tt.phase},
			}
			kubeClient := fake.NewClientBuilder().WithObjects(pod).Build()
			output, err := sqlJobOutput(t.Context(), kubeClient, podClient, job)
			if (err != nil) != tt.wantError {
				t.Errorf("unexpected result: %q, %v", output, err)
			}
			if !tt.wantError && output != "appdb|appdb_rw" {
				t.Errorf("unexpected SQL output: %q", output)
			}
		})
	}
}
