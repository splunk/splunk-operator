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

package controller

import (
	"context"

	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// kvServiceNamespaceMapper enqueues all consumers of a kind in the namespace.
// It deliberately includes disabled namespaces so removing labels clears defaults.
func kvServiceNamespaceMapper(c client.Client, list client.ObjectList) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		namespace, ok := obj.(*corev1.Namespace)
		if !ok {
			return nil
		}
		if _, err := splutil.GetTenantName(namespace); err != nil {
			return nil
		}
		items := list.DeepCopyObject().(client.ObjectList)
		if err := c.List(ctx, items, client.InNamespace(namespace.Name)); err != nil {
			log.FromContext(ctx).Error(err, "Unable to list KVService consumers", "namespace", namespace.Name)
			return nil
		}
		objects, err := meta.ExtractList(items)
		if err != nil {
			return nil
		}
		requests := make([]reconcile.Request, 0, len(objects))
		for _, object := range objects {
			metadata, err := meta.Accessor(object)
			if err != nil {
				continue
			}
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: metadata.GetNamespace(), Name: metadata.GetName()}})
		}
		return requests
	}
}
