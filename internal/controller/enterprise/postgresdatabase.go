// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package controller

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Search-tier controllers consume producer publications without owning them.
// +kubebuilder:rbac:groups=platform.splunk.com,resources=postgresdatabases;postgresclusters,verbs=get;list;watch

const postgresDatabaseRefIndex = "spec.postgresDatabaseMounts.postgresDatabaseRef.name"

type postgresDatabaseWatch struct {
	client  client.Client
	newList func() client.ObjectList
}

func postgresMountSpecs(object client.Object) []enterpriseApi.PostgresDatabaseMountSpec {
	switch cr := object.(type) {
	case *enterpriseApi.Standalone:
		return cr.Spec.PostgresDatabaseMounts
	case *enterpriseApi.SearchHeadCluster:
		return cr.Spec.PostgresDatabaseMounts
	default:
		return nil
	}
}

// Producer status-only writes must pass the controller's global event filter.
func postgresDatabaseChangedPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		switch object.(type) {
		case *platformApi.PostgresDatabase, *platformApi.PostgresCluster:
			return true
		default:
			return false
		}
	})
}

func setupPostgresDatabaseWatch(mgr ctrl.Manager, bldr *builder.Builder, consumer client.Object, newList func() client.ObjectList) error {
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		return nil
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), consumer, postgresDatabaseRefIndex, func(object client.Object) []string {
		names := make([]string, 0)
		seen := make(map[string]bool)
		for _, mount := range postgresMountSpecs(object) {
			name := mount.PostgresDatabaseRef.Name
			if name != "" && !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
		return names
	}); err != nil {
		return err
	}
	watch := postgresDatabaseWatch{client: mgr.GetClient(), newList: newList}
	bldr.Watches(&platformApi.PostgresDatabase{}, handler.EnqueueRequestsFromMapFunc(watch.mapDatabase)).
		Watches(&platformApi.PostgresCluster{}, handler.EnqueueRequestsFromMapFunc(watch.mapResource)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(watch.mapResource)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(watch.mapResource))
	return nil
}

func (w postgresDatabaseWatch) mapDatabase(ctx context.Context, object client.Object) []reconcile.Request {
	list := w.newList()
	if err := w.client.List(ctx, list, client.InNamespace(object.GetNamespace()), client.MatchingFields{postgresDatabaseRefIndex: object.GetName()}); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to map PostgresDatabase consumers", "error", err)
		return nil
	}
	objects, err := meta.ExtractList(list)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to extract PostgresDatabase consumers", "error", err)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(objects))
	for _, object := range objects {
		consumer, ok := object.(client.Object)
		if ok {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(consumer)})
		}
	}
	return requests
}

// A missing/recreated publication may not change producer status. Follow its
// published references for resource events too, including external Secrets.
func (w postgresDatabaseWatch) mapResource(ctx context.Context, object client.Object) []reconcile.Request {
	var databases platformApi.PostgresDatabaseList
	if err := w.client.List(ctx, &databases, client.InNamespace(object.GetNamespace())); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to map PostgresDatabase publication", "error", err)
		return nil
	}
	seen := make(map[client.ObjectKey]bool)
	var requests []reconcile.Request
	for _, db := range databases.Items {
		matches := false
		switch object.(type) {
		case *platformApi.PostgresCluster:
			matches = db.Spec.ClusterRef.Name == object.GetName()
		case *corev1.Secret:
			matches = db.Spec.ClusterRef.Name != "" && db.Spec.ClusterRef.Name+"-ca" == object.GetName()
		}
		for _, info := range db.Status.Databases {
			if matches {
				break
			}
			switch object.(type) {
			case *corev1.ConfigMap:
				matches = info.ConfigMapRef != nil && info.ConfigMapRef.Name == object.GetName()
			case *corev1.Secret:
				matches = (info.RWUserSecretRef != nil && info.RWUserSecretRef.Name == object.GetName()) ||
					(info.AdminUserSecretRef != nil && info.AdminUserSecretRef.Name == object.GetName())
			}
			if matches {
				break
			}
		}
		if !matches {
			continue
		}
		for _, request := range w.mapDatabase(ctx, &db) {
			if !seen[request.NamespacedName] {
				seen[request.NamespacedName] = true
				requests = append(requests, request)
			}
		}
	}
	return requests
}
