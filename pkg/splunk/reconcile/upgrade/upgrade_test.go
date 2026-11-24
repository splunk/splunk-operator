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

package upgrade

import (
	"context"
	"errors"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace = "test"
	oldImage      = "splunk/splunk:old"
	newImage      = "splunk/splunk:new"
)

func newTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return newTestClientWithInterceptor(t, interceptor.Funcs{}, objs...)
}

func newTestClientWithInterceptor(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) client.Client {
	t.Helper()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	// The default managed-fields tracker rejects the uint64 fields in Splunk CR specs.
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).
		WithObjects(objs...).
		WithInterceptorFuncs(funcs).
		Build()
}

func statefulSet(instanceType splcommon.InstanceType, crName, image string) *appsv1.StatefulSet {
	labels := map[string]string{"app": crName}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: splutil.GetSplunkStatefulsetName(instanceType, crName), Namespace: testNamespace},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: image}}},
			},
		},
	}
}

func clusterManager(name string) *enterpriseApi.ClusterManager {
	return &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Status:     enterpriseApi.ClusterManagerStatus{Phase: enterpriseApi.PhaseReady},
	}
}

func searchHeadCluster(name, managerName string, phase enterpriseApi.Phase) *enterpriseApi.SearchHeadCluster {
	shc := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	shc.Spec.ClusterManagerRef = corev1.ObjectReference{Name: managerName}
	shc.Status.Phase = phase
	return shc
}

func indexerCluster(name, managerName, image string) *enterpriseApi.IndexerCluster {
	idx := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	idx.Spec.Image = image
	idx.Spec.ClusterManagerRef = corev1.ObjectReference{Name: managerName}
	idx.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))
	return idx
}

func singleSiteClusterInfo(context.Context) (*splclient.ClusterInfo, error) {
	return &splclient.ClusterInfo{MultiSite: "false"}, nil
}

func TestMonitoringConsoleDependencyListErrors(t *testing.T) {
	for _, kind := range []string{"ClusterManagerList", "SearchHeadClusterList", "IndexerClusterList", "StandaloneList"} {
		t.Run(kind, func(t *testing.T) {
			want := errors.New("dependency API unavailable")
			c := newTestClientWithInterceptor(t, dependencyListError(kind, want))
			mc := &enterpriseApi.MonitoringConsole{ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testNamespace}}
			mc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("MonitoringConsole"))
			proceed, err := UpgradePathValidation(context.Background(), c, mc, mc.Spec.CommonSplunkSpec, nil)
			if proceed || !errors.Is(err, want) {
				t.Fatalf("UpgradePathValidation() = %v, %v; want false, %v", proceed, err, want)
			}
		})
	}
}

func dependencyListError(kind string, want error) interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if list.GetObjectKind().GroupVersionKind().Kind == kind {
				return want
			}
			// Typed lists do not necessarily have their GVK populated yet.
			gvks, _, err := c.Scheme().ObjectKinds(list)
			if err != nil {
				return err
			}
			for _, gvk := range gvks {
				if gvk.Kind == kind {
					return want
				}
			}
			return c.List(ctx, list, opts...)
		},
	}
}

// TestIndexerUpgradeWaitsForEverySearchHeadCluster covers the documented
// ClusterManager -> SearchHeadCluster -> IndexerCluster order: an indexer image
// change must wait until every SHC on the same ClusterManager is Ready on the
// target image, not just the first one found.
func TestIndexerUpgradeWaitsForEverySearchHeadCluster(t *testing.T) {
	tests := []struct {
		name         string
		shcB         *enterpriseApi.SearchHeadCluster
		shcBImage    string
		wantContinue bool
	}{
		{"second SHC still on old image", searchHeadCluster("shc-b", "cm", enterpriseApi.PhaseReady), oldImage, false},
		{"second SHC not ready", searchHeadCluster("shc-b", "cm", enterpriseApi.PhaseUpdating), newImage, false},
		{"all SHCs ready on target image", searchHeadCluster("shc-b", "cm", enterpriseApi.PhaseReady), newImage, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := indexerCluster("idx", "cm", newImage)
			c := newTestClient(t,
				clusterManager("cm"), statefulSet(splcommon.SplunkClusterManager, "cm", newImage),
				searchHeadCluster("shc-a", "cm", enterpriseApi.PhaseReady), statefulSet(splcommon.SplunkSearchHead, "shc-a", newImage),
				tt.shcB, statefulSet(splcommon.SplunkSearchHead, "shc-b", tt.shcBImage),
				statefulSet(splcommon.SplunkIndexer, "idx", oldImage),
			)

			got, err := UpgradePathValidation(context.TODO(), c, idx, idx.Spec.CommonSplunkSpec, singleSiteClusterInfo)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantContinue {
				t.Fatalf("continueReconcile = %v, want %v", got, tt.wantContinue)
			}
		})
	}
}

// TestIndexerWithoutImageChangeIgnoresSearchHeadCluster ensures routine indexer
// reconciles (scaling, recovery) are not blocked by a search-head outage.
func TestIndexerWithoutImageChangeIgnoresSearchHeadCluster(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)
	c := newTestClient(t,
		clusterManager("cm"), statefulSet(splcommon.SplunkClusterManager, "cm", newImage),
		searchHeadCluster("shc", "cm", enterpriseApi.PhaseError), statefulSet(splcommon.SplunkSearchHead, "shc", oldImage),
		statefulSet(splcommon.SplunkIndexer, "idx", newImage),
	)

	got, err := UpgradePathValidation(context.TODO(), c, idx, idx.Spec.CommonSplunkSpec, singleSiteClusterInfo)
	if err != nil || !got {
		t.Fatalf("expected continue without error, got continue=%v err=%v", got, err)
	}
}

// TestIndexerUpgradeFailsWhenSearchHeadImageUnreadable ensures a SHC whose
// StatefulSet cannot be read stops the upgrade instead of being skipped.
func TestIndexerUpgradeFailsWhenSearchHeadImageUnreadable(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)
	c := newTestClient(t,
		clusterManager("cm"), statefulSet(splcommon.SplunkClusterManager, "cm", newImage),
		searchHeadCluster("shc", "cm", enterpriseApi.PhaseReady),
		statefulSet(splcommon.SplunkIndexer, "idx", oldImage),
	)

	got, err := UpgradePathValidation(context.TODO(), c, idx, idx.Spec.CommonSplunkSpec, singleSiteClusterInfo)
	if err == nil || got {
		t.Fatalf("expected error and no continue, got continue=%v err=%v", got, err)
	}
	// Wrapping must keep the original cause reachable (the SHC StatefulSet is missing).
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected the underlying NotFound to survive wrapping, got %v", err)
	}
}

func TestIndexerImageChanging(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)

	changing, err := indexerImageChanging(context.TODO(), newTestClient(t), idx, newImage)
	if err != nil || changing {
		t.Fatalf("missing StatefulSet: got changing=%v err=%v, want false nil", changing, err)
	}

	c := newTestClient(t, statefulSet(splcommon.SplunkIndexer, "idx", oldImage))
	if changing, err = indexerImageChanging(context.TODO(), c, idx, newImage); err != nil || !changing {
		t.Fatalf("old image: got changing=%v err=%v, want true nil", changing, err)
	}
	if changing, err = indexerImageChanging(context.TODO(), c, idx, oldImage); err != nil || changing {
		t.Fatalf("same image: got changing=%v err=%v, want false nil", changing, err)
	}
}

// TestIndexerUpgradeFailsWhenIndexerImageUnreadable ensures an API error reading
// the indexer StatefulSet (other than NotFound) stops validation.
func TestIndexerUpgradeFailsWhenIndexerImageUnreadable(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)
	indexerStatefulSet := splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, "idx")
	apiErr := errors.New("apiserver unavailable")
	c := newTestClientWithInterceptor(t, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Name == indexerStatefulSet {
				return apiErr
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}, clusterManager("cm"), statefulSet(splcommon.SplunkClusterManager, "cm", newImage))

	got, err := UpgradePathValidation(context.TODO(), c, idx, idx.Spec.CommonSplunkSpec, singleSiteClusterInfo)
	if err == nil || got {
		t.Fatalf("expected error and no continue, got continue=%v err=%v", got, err)
	}
	if !errors.Is(err, apiErr) {
		t.Fatalf("expected the underlying API error to survive wrapping, got %v", err)
	}
}

func TestSearchHeadClusterStates(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)
	c := newTestClient(t,
		searchHeadCluster("shc-a", "cm", enterpriseApi.PhaseReady), statefulSet(splcommon.SplunkSearchHead, "shc-a", newImage),
		searchHeadCluster("shc-b", "cm", enterpriseApi.PhaseUpdating), statefulSet(splcommon.SplunkSearchHead, "shc-b", oldImage),
		searchHeadCluster("shc-other", "other-cm", enterpriseApi.PhaseReady),
	)
	ref := corev1.ObjectReference{Name: "cm"}
	logger := testLogger{}

	states, err := searchHeadClusterStates(context.TODO(), c, idx, ref, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d SHC states, want 2 (SHC on another ClusterManager must be ignored): %+v", len(states), states)
	}

	first, err := searchHeadClusterState(context.TODO(), c, idx, ref, nil, logger)
	if err != nil || first == nil || first.Name != states[0].Name {
		t.Fatalf("searchHeadClusterState = %+v, %v; want first of %+v", first, err, states)
	}

	none, err := searchHeadClusterState(context.TODO(), c, idx, corev1.ObjectReference{Name: "missing"}, nil, logger)
	if err != nil || none != nil {
		t.Fatalf("no matching SHC: got %+v, %v; want nil nil", none, err)
	}
}

// TestSearchHeadClusterStatesListErrors covers the List failure paths: NotFound
// means there are no SearchHeadClusters, any other error is wrapped and returned.
func TestSearchHeadClusterStatesListErrors(t *testing.T) {
	idx := indexerCluster("idx", "cm", newImage)
	ref := corev1.ObjectReference{Name: "cm"}
	listErr := errors.New("apiserver unavailable")
	listFails := func(err error) client.Client {
		return newTestClientWithInterceptor(t, interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*enterpriseApi.SearchHeadClusterList); ok {
					return err
				}
				return c.List(ctx, list, opts...)
			},
		})
	}

	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "enterprise.splunk.com", Resource: "searchheadclusters"}, "")
	states, err := searchHeadClusterStates(context.TODO(), listFails(notFound), idx, ref, nil, testLogger{})
	if err != nil || states != nil {
		t.Fatalf("NotFound list: got %+v, %v; want nil nil", states, err)
	}

	_, err = searchHeadClusterStates(context.TODO(), listFails(listErr), idx, ref, nil, testLogger{})
	if !errors.Is(err, listErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}

	if _, err = searchHeadClusterState(context.TODO(), listFails(listErr), idx, ref, nil, testLogger{}); !errors.Is(err, listErr) {
		t.Fatalf("searchHeadClusterState should propagate the wrapped error, got %v", err)
	}
}

type testLogger struct{}

func (testLogger) ErrorContext(context.Context, string, ...any) {}
