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
package core

import (
	"context"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// endpointOperation keeps the values of one validated preparation or
// publication intent together through mutation, read-back, and status write.
type endpointOperation interface {
	intent() endpointOperationIntent
	validateCurrent(context.Context, client.Reader, *platformv1alpha1.PostgresDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	stamp(*corev1.ConfigMap)
	stampMatches(*corev1.ConfigMap) bool
	verifyBeforeAck(context.Context, client.Reader, *platformv1alpha1.PostgresDatabase) error
	existingAck(*platformv1alpha1.PostgresDatabase) *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus
	setAck(*platformv1alpha1.PostgresDatabase, *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus)
}

type endpointOperationIntent struct {
	routing     map[string]string
	attemptID   string
	generation  int64
	fingerprint string
}

type preparationOperation struct{ endpointOperationIntent }

func (op preparationOperation) intent() endpointOperationIntent { return op.endpointOperationIntent }

func (op preparationOperation) validateCurrent(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return checkCurrentPreparation(ctx, reader, db, bg, op.generation, op.fingerprint)
}

func (preparationOperation) stamp(*corev1.ConfigMap) {
	// Preparation keeps blue routing and must not stamp it as a green publication.
}
func (preparationOperation) stampMatches(*corev1.ConfigMap) bool { return true }
func (preparationOperation) verifyBeforeAck(context.Context, client.Reader, *platformv1alpha1.PostgresDatabase) error {
	return nil
}
func (preparationOperation) existingAck(db *platformv1alpha1.PostgresDatabase) *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus {
	return db.Status.EndpointPreparation
}
func (preparationOperation) setAck(db *platformv1alpha1.PostgresDatabase, ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
	db.Status.EndpointPreparation = ack
}

type publicationOperation struct {
	endpointOperationIntent
	greenEnvironment string
}

func (op publicationOperation) intent() endpointOperationIntent { return op.endpointOperationIntent }

func (op publicationOperation) validateCurrent(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return checkCurrentPublication(ctx, reader, db, bg, op.generation, op.fingerprint, op.routing)
}

func (op publicationOperation) stamp(cm *corev1.ConfigMap) {
	stampEndpointPublication(cm, op.greenEnvironment, op.attemptID, op.generation, op.fingerprint)
}

func (op publicationOperation) stampMatches(cm *corev1.ConfigMap) bool {
	return endpointStampMatches(cm, op.greenEnvironment, op.attemptID, op.generation, op.fingerprint)
}

func (op publicationOperation) verifyBeforeAck(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase) error {
	_, err := verifyEndpointConfigMaps(ctx, reader, db, op)
	return err
}

func (publicationOperation) existingAck(db *platformv1alpha1.PostgresDatabase) *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus {
	return db.Status.EndpointPublication
}
func (publicationOperation) setAck(db *platformv1alpha1.PostgresDatabase, ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
	db.Status.EndpointPublication = ack
}
