// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package upgrade

import (
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSupportedSHCImageDifferenceRecordsDurableWorkflow(t *testing.T) {
	input := supportedImageUpgradeInput()

	decision := ClassifySHCImageUpgrade(input)

	if decision.Classification != SHCImageUpgradeRecord ||
		decision.Operation == nil {
		t.Fatalf("decision = %#v, want recorded image-upgrade operation", decision)
	}
	operation := decision.Operation
	if operation.OperationID !=
		"image-upgrade:splunk-example-search-head:revision-2" ||
		operation.StatefulSetName != "splunk-example-search-head" ||
		operation.DesiredRevision != "revision-2" ||
		operation.SourceImage != "splunk/splunk:9.4.0" ||
		operation.TargetImage != "splunk/splunk:10.0.0" ||
		operation.TargetReplicas != 3 {
		t.Fatalf("operation identity = %#v", operation)
	}
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization ||
		operation.Reason !=
			enterpriseApi.SearchHeadClusterImageUpgradeReasonWorkflowRecorded {
		t.Fatalf("operation state = %#v", operation)
	}
	if operation.StartedAt == nil || operation.PhaseStartedAt == nil ||
		operation.LastTransitionTime == nil ||
		operation.InitializationIntentAt != nil ||
		operation.InitializationAttemptCount != 0 {
		t.Fatalf("workflow did not preserve the pre-side-effect barrier: %#v", operation)
	}
}

func TestPrivateRegistrySHCImageDifferencePreservesOpaqueImageReferences(
	t *testing.T,
) {
	input := supportedImageUpgradeInput()
	sourceImage := "registry.airgap.example:5000/splunk/splunk@sha256:source"
	targetImage := "registry.airgap.example:5000/splunk/splunk@sha256:target"
	for index := range input.Pods {
		input.Pods[index].Image = sourceImage
	}
	input.TargetImage = targetImage

	decision := ClassifySHCImageUpgrade(input)

	if decision.Classification != SHCImageUpgradeRecord ||
		decision.Operation == nil {
		t.Fatalf("decision = %#v, want recorded private-registry image upgrade", decision)
	}
	if decision.Operation.SourceImage != sourceImage ||
		decision.Operation.TargetImage != targetImage {
		t.Fatalf(
			"image references changed: source=%q target=%q",
			decision.Operation.SourceImage,
			decision.Operation.TargetImage,
		)
	}
}

func TestUnchangedSHCImageIsOrdinaryTemplateRollout(t *testing.T) {
	input := supportedImageUpgradeInput()
	input.TargetImage = input.Pods[0].Image

	decision := ClassifySHCImageUpgrade(input)

	assertImageUpgradeClassification(
		t,
		decision,
		SHCImageUpgradeOrdinaryRollout,
		"",
	)
}

func TestSHCImageUpgradeRequiresAuthoritativePathDecision(t *testing.T) {
	tests := []struct {
		name         string
		pathDecision SHCImageUpgradePathDecision
		reason       enterpriseApi.SearchHeadClusterImageUpgradeReason
	}{
		{
			name:         "unsupported",
			pathDecision: SHCImageUpgradePathUnsupported,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonUnsupportedUpgradePath,
		},
		{
			name:         "unknown",
			pathDecision: SHCImageUpgradePathUnknown,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonUnknownUpgradePath,
		},
		{
			name: "missing decision",
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonUnknownUpgradePath,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := supportedImageUpgradeInput()
			input.PathDecision = test.pathDecision

			decision := ClassifySHCImageUpgrade(input)

			assertImageUpgradeClassification(
				t,
				decision,
				SHCImageUpgradeBlock,
				test.reason,
			)
		})
	}
}

func TestSHCImageUpgradeBlocksAmbiguousSourceState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SHCImageUpgradeClassificationInput)
		class  SHCImageUpgradeClassification
		reason enterpriseApi.SearchHeadClusterImageUpgradeReason
	}{
		{
			name: "mixed source images",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.Pods[2].Image = "splunk/splunk:9.3.0"
			},
			class: SHCImageUpgradeBlock,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonMixedSourceImages,
		},
		{
			name: "ordinal already at desired revision",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.Pods[2].Revision = input.DesiredRevision
			},
			class: SHCImageUpgradeBlock,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonRevisionConflict,
		},
		{
			name: "source pod unavailable",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.Pods[2].Ready = false
			},
			class: SHCImageUpgradeWait,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonClusterNotReady,
		},
		{
			name: "another planned owner",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.ConflictingPlannedOperation = true
			},
			class: SHCImageUpgradeBlock,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := supportedImageUpgradeInput()
			test.mutate(&input)

			decision := ClassifySHCImageUpgrade(input)

			assertImageUpgradeClassification(
				t,
				decision,
				test.class,
				test.reason,
			)
		})
	}
}

func TestExistingSHCImageUpgradeBlocksDesiredStateConflicts(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*SHCImageUpgradeClassificationInput)
		reason enterpriseApi.SearchHeadClusterImageUpgradeReason
	}{
		{
			name: "revision changes",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.DesiredRevision = "revision-3"
			},
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonRevisionConflict,
		},
		{
			name: "target image changes",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.TargetImage = "splunk/splunk:10.1.0"
			},
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonTargetImageConflict,
		},
		{
			name: "replicas change",
			mutate: func(input *SHCImageUpgradeClassificationInput) {
				input.TargetReplicas = 5
			},
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonReplicaConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := supportedImageUpgradeInput()
			operation := recordedImageUpgrade(input)
			input.Current = operation
			input.Now = now
			test.mutate(&input)

			decision := ClassifySHCImageUpgrade(input)

			assertImageUpgradeClassification(
				t,
				decision,
				SHCImageUpgradeBlock,
				test.reason,
			)
			if decision.Operation == nil ||
				decision.Operation.Phase !=
					enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
				decision.Operation.LastTransitionTime == nil {
				t.Fatalf("blocked operation = %#v", decision.Operation)
			}
			if operation.Phase !=
				enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization {
				t.Fatal("classifier mutated persisted input operation")
			}
		})
	}
}

func TestExistingSHCImageUpgradeResumesRecordedIdentity(t *testing.T) {
	input := supportedImageUpgradeInput()
	operation := recordedImageUpgrade(input)
	operation.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers
	input.Current = operation
	input.Pods[2].Image = input.TargetImage
	input.Pods[2].Revision = input.DesiredRevision

	decision := ClassifySHCImageUpgrade(input)

	assertImageUpgradeClassification(
		t,
		decision,
		SHCImageUpgradeResume,
		enterpriseApi.SearchHeadClusterImageUpgradeReasonWorkflowRecorded,
	)
	if decision.Operation == operation {
		t.Fatal("resumed operation aliases persisted status")
	}
}

func TestBlockedOrFailedSHCImageUpgradeRemainsFailClosed(t *testing.T) {
	tests := []enterpriseApi.SearchHeadClusterImageUpgradePhase{
		enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked,
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed,
	}
	for _, phase := range tests {
		t.Run(string(phase), func(t *testing.T) {
			input := supportedImageUpgradeInput()
			operation := recordedImageUpgrade(input)
			operation.Phase = phase
			operation.Reason =
				enterpriseApi.SearchHeadClusterImageUpgradeReasonRevisionConflict
			input.Current = operation

			decision := ClassifySHCImageUpgrade(input)

			assertImageUpgradeClassification(
				t,
				decision,
				SHCImageUpgradeBlock,
				enterpriseApi.SearchHeadClusterImageUpgradeReasonRevisionConflict,
			)
		})
	}
}

func TestSHCImageUpgradeWaitsForUpdateRevision(t *testing.T) {
	input := supportedImageUpgradeInput()
	input.DesiredRevision = ""

	decision := ClassifySHCImageUpgrade(input)

	assertImageUpgradeClassification(
		t,
		decision,
		SHCImageUpgradeWait,
		enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
	)
}

func supportedImageUpgradeInput() SHCImageUpgradeClassificationInput {
	return SHCImageUpgradeClassificationInput{
		StatefulSetName: "splunk-example-search-head",
		DesiredRevision: "revision-2",
		TargetImage:     "splunk/splunk:10.0.0",
		TargetReplicas:  3,
		PathDecision:    SHCImageUpgradePathSupported,
		Now:             time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC),
		Pods: []SHCImageUpgradePod{
			{
				Ordinal:  0,
				Exists:   true,
				Ready:    true,
				Revision: "revision-1",
				Image:    "splunk/splunk:9.4.0",
			},
			{
				Ordinal:  1,
				Exists:   true,
				Ready:    true,
				Revision: "revision-1",
				Image:    "splunk/splunk:9.4.0",
			},
			{
				Ordinal:  2,
				Exists:   true,
				Ready:    true,
				Revision: "revision-1",
				Image:    "splunk/splunk:9.4.0",
			},
		},
	}
}

func recordedImageUpgrade(
	input SHCImageUpgradeClassificationInput,
) *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
	timestamp := metav1.NewTime(input.Now.Add(-time.Minute))
	return &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
		OperationID:     "image-upgrade:splunk-example-search-head:revision-2",
		StatefulSetName: input.StatefulSetName,
		DesiredRevision: input.DesiredRevision,
		SourceImage:     "splunk/splunk:9.4.0",
		TargetImage:     input.TargetImage,
		TargetReplicas:  input.TargetReplicas,
		Phase: enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingInitialization,
		Reason: enterpriseApi.
			SearchHeadClusterImageUpgradeReasonWorkflowRecorded,
		StartedAt:          &timestamp,
		PhaseStartedAt:     &timestamp,
		LastTransitionTime: &timestamp,
	}
}

func assertImageUpgradeClassification(
	t *testing.T,
	decision SHCImageUpgradeClassificationDecision,
	classification SHCImageUpgradeClassification,
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
) {
	t.Helper()
	if decision.Classification != classification || decision.Reason != reason {
		t.Fatalf(
			"classification = %q reason = %q, want %q/%q: %#v",
			decision.Classification,
			decision.Reason,
			classification,
			reason,
			decision,
		)
	}
	if classification != SHCImageUpgradeRecord &&
		classification != SHCImageUpgradeResume &&
		decision.Operation != nil {
		// Blocked existing workflows retain their durable operation.
		if decision.Operation.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked &&
			decision.Operation.Phase !=
				enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed {
			t.Fatalf("unexpected operation for classification: %#v", decision)
		}
	}
}
