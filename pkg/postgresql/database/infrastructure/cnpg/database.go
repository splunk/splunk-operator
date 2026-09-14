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

// Package cnpg contains narrow Kubernetes operations for CNPG resources owned
// by PostgresDatabase.
package cnpg

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const AnnotationRetainedFrom = "platform.splunk.com/retained-from"

// ApplyDatabase creates or updates a CNPG Database while preserving the
// existing retained-resource adoption behavior.
func ApplyDatabase(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner *platformv1alpha1.PostgresDatabase,
	resourceName string,
	desired func(cnpgv1.Database) (cnpgv1.DatabaseSpec, error),
) (cnpgv1.Database, bool, error) {
	database := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: owner.Namespace},
	}
	reAdopted := false
	_, err := controllerutil.CreateOrUpdate(ctx, c, database, func() error {
		desiredSpec, err := desired(*database)
		if err != nil {
			return err
		}
		database.Spec = desiredSpec
		reAdopted = database.Annotations[AnnotationRetainedFrom] == owner.Name
		if reAdopted {
			delete(database.Annotations, AnnotationRetainedFrom)
		}
		if database.CreationTimestamp.IsZero() || reAdopted || !metav1.IsControlledBy(database, owner) {
			return controllerutil.SetControllerReference(owner, database, scheme)
		}
		return nil
	})
	return *database, reAdopted, err
}

func GetDatabase(ctx context.Context, c client.Client, key types.NamespacedName) (cnpgv1.Database, error) {
	database := cnpgv1.Database{}
	err := c.Get(ctx, key, &database)
	return database, err
}
