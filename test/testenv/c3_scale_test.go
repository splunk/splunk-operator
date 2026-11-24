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

package testenv

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func scaleFixture(t *testing.T, failedRequest string) (*TestCaseEnv, *Deployment, *[]string, error) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	shc := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo-shc", Namespace: "ns", Generation: 2}}
	shc.Spec.Replicas = 3
	shc.Status.Replicas, shc.Status.ReadyReplicas = 3, 3
	idxc := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo-idxc", Namespace: "ns"}}
	idxc.Spec.Replicas = 3
	injected := errors.New("injected update failure")
	var updates []string
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shc, idxc).WithStatusSubresource(shc, idxc).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				kinds, _, err := scheme.ObjectKinds(obj)
				if err == nil && len(kinds) > 0 {
					obj.GetObjectKind().SetGroupVersionKind(kinds[0])
				}
				return err
			},
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				updates = append(updates, obj.GetName())
				if obj.GetName() == failedRequest {
					return injected
				}
				return c.Update(ctx, obj, opts...)
			},
		}).Build()
	env := &TestCaseEnv{namespace: "ns", name: "ns", kubeClient: cl, Log: logr.Discard()}
	return env, &Deployment{name: "demo", testenv: env, testTimeout: time.Second}, &updates, injected
}

func TestC3ScaleRequestsPrecedeReadiness(t *testing.T) {
	env, dep, updates, _ := scaleFixture(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := env.RequestC3ScaleUp(ctx, dep, 4, 4); err != nil {
		t.Fatal(err)
	}
	if len(*updates) != 2 || (*updates)[0] != "demo-shc" || (*updates)[1] != "demo-idxc" {
		t.Fatalf("updates = %v", *updates)
	}
	shc := &enterpriseApi.SearchHeadCluster{}
	idxc := &enterpriseApi.IndexerCluster{}
	if err := dep.GetInstance(ctx, "demo-shc", shc); err != nil {
		t.Fatal(err)
	}
	if err := dep.GetInstance(ctx, "demo-idxc", idxc); err != nil {
		t.Fatal(err)
	}
	if shc.Spec.Replicas != 4 || idxc.Spec.Replicas != 4 || shc.Status.ReadyReplicas != 3 {
		t.Fatal("both requests must be submitted while SHC is still reconciling")
	}
	waiting, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if err := env.WaitForSearchHeadClusterScaleComplete(waiting, dep, 4); err == nil {
		t.Fatal("completion waiter accepted stale generation/replica readiness")
	}
}

func TestC3ScaleRequestFailures(t *testing.T) {
	for _, name := range []string{"demo-shc", "demo-idxc"} {
		t.Run(name, func(t *testing.T) {
			env, dep, updates, injected := scaleFixture(t, name)
			if err := env.RequestC3ScaleUp(context.Background(), dep, 4, 4); !errors.Is(err, injected) {
				t.Fatalf("got %v, want injected error", err)
			}
			for _, updated := range *updates {
				if name == "demo-shc" && updated != "demo-shc" {
					t.Fatal("indexer request followed failed SHC request")
				}
			}
		})
	}
}
