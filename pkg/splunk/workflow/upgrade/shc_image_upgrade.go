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
	"fmt"
	"sort"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SHCImageUpgradePathDecision is supplied by the authoritative compatibility
// boundary. Image tags alone are not treated as compatibility evidence.
type SHCImageUpgradePathDecision string

const (
	SHCImageUpgradePathSupported   SHCImageUpgradePathDecision = "Supported"
	SHCImageUpgradePathUnsupported SHCImageUpgradePathDecision = "Unsupported"
	SHCImageUpgradePathUnknown     SHCImageUpgradePathDecision = "Unknown"
)

// SHCImageUpgradeClassification identifies whether the observed template
// change should create or resume a cluster-scoped image-upgrade workflow.
type SHCImageUpgradeClassification string

const (
	SHCImageUpgradeWait            SHCImageUpgradeClassification = "Wait"
	SHCImageUpgradeOrdinaryRollout SHCImageUpgradeClassification = "OrdinaryRollout"
	SHCImageUpgradeRecord          SHCImageUpgradeClassification = "Record"
	SHCImageUpgradeResume          SHCImageUpgradeClassification = "Resume"
	SHCImageUpgradeBlock           SHCImageUpgradeClassification = "Block"
)

// SHCImageUpgradePod is the bounded Pod observation required for image
// classification. Runtime image IDs are intentionally excluded.
type SHCImageUpgradePod struct {
	Ordinal  int32
	Exists   bool
	Ready    bool
	Deleting bool
	Revision string
	Image    string
}

// SHCImageUpgradeClassificationInput is a point-in-time observation used by
// the pure classifier.
type SHCImageUpgradeClassificationInput struct {
	StatefulSetName             string
	DesiredRevision             string
	TargetImage                 string
	TargetReplicas              int32
	Pods                        []SHCImageUpgradePod
	PathDecision                SHCImageUpgradePathDecision
	ConflictingPlannedOperation bool
	Current                     *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Now                         time.Time
}

// SHCImageUpgradeClassificationDecision is a side-effect-free result.
type SHCImageUpgradeClassificationDecision struct {
	Classification SHCImageUpgradeClassification
	Operation      *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Reason         enterpriseApi.SearchHeadClusterImageUpgradeReason
	Message        string
}

// ClassifySHCImageUpgrade records only unambiguous, supported image changes.
// It never mutates the input status.
func ClassifySHCImageUpgrade(
	input SHCImageUpgradeClassificationInput,
) SHCImageUpgradeClassificationDecision {
	if input.Current != nil &&
		input.Current.Phase != enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted {
		return classifyExistingSHCImageUpgrade(input)
	}
	if input.Current != nil &&
		input.Current.DesiredRevision == input.DesiredRevision &&
		input.Current.TargetImage == input.TargetImage &&
		input.Current.TargetReplicas == input.TargetReplicas {
		return SHCImageUpgradeClassificationDecision{
			Classification: SHCImageUpgradeResume,
			Operation:      input.Current.DeepCopy(),
			Reason:         input.Current.Reason,
			Message:        input.Current.Message,
		}
	}

	if input.DesiredRevision == "" || input.StatefulSetName == "" ||
		input.TargetImage == "" || input.TargetReplicas <= 0 {
		return imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeWait,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"wait for a complete StatefulSet image-upgrade observation",
		)
	}
	if input.ConflictingPlannedOperation {
		return imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeBlock,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation,
			"another planned operation owns Search Head Cluster lifecycle coordination",
		)
	}

	pods, decision := validateImageUpgradeSourcePods(input)
	if decision != nil {
		return *decision
	}
	sourceImage := pods[0].Image
	if sourceImage == input.TargetImage {
		return imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeOrdinaryRollout,
			"",
			"StatefulSet template revision changed without changing the Splunk image",
		)
	}
	for _, pod := range pods {
		if pod.Revision == input.DesiredRevision {
			return imageUpgradeClassificationWithoutOperation(
				SHCImageUpgradeBlock,
				enterpriseApi.SearchHeadClusterImageUpgradeReasonRevisionConflict,
				"an ordinal reached the desired revision before image-upgrade ownership was recorded",
			)
		}
	}

	switch input.PathDecision {
	case SHCImageUpgradePathSupported:
	case SHCImageUpgradePathUnsupported:
		return imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeBlock,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonUnsupportedUpgradePath,
			"the requested Search Head Cluster image transition is unsupported",
		)
	default:
		return imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeBlock,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonUnknownUpgradePath,
			"the requested Search Head Cluster image transition has no authoritative support decision",
		)
	}

	timestamp := metav1.NewTime(input.Now)
	operation := &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
		OperationID: fmt.Sprintf(
			"image-upgrade:%s:%s",
			input.StatefulSetName,
			input.DesiredRevision,
		),
		StatefulSetName:    input.StatefulSetName,
		DesiredRevision:    input.DesiredRevision,
		SourceImage:        sourceImage,
		TargetImage:        input.TargetImage,
		TargetReplicas:     input.TargetReplicas,
		Phase:              enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization,
		Reason:             enterpriseApi.SearchHeadClusterImageUpgradeReasonWorkflowRecorded,
		Message:            "recorded Search Head Cluster image-upgrade workflow",
		StartedAt:          &timestamp,
		PhaseStartedAt:     &timestamp,
		LastTransitionTime: &timestamp,
	}
	return SHCImageUpgradeClassificationDecision{
		Classification: SHCImageUpgradeRecord,
		Operation:      operation,
		Reason:         operation.Reason,
		Message:        operation.Message,
	}
}

func classifyExistingSHCImageUpgrade(
	input SHCImageUpgradeClassificationInput,
) SHCImageUpgradeClassificationDecision {
	operation := input.Current.DeepCopy()
	if operation.Phase == enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		operation.Phase == enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed {
		return SHCImageUpgradeClassificationDecision{
			Classification: SHCImageUpgradeBlock,
			Operation:      operation,
			Reason:         operation.Reason,
			Message:        operation.Message,
		}
	}
	switch {
	case operation.DesiredRevision != input.DesiredRevision:
		blockImageUpgrade(
			operation,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonRevisionConflict,
			"desired StatefulSet revision changed during the image-upgrade workflow",
			input.Now,
		)
	case operation.TargetImage != input.TargetImage:
		blockImageUpgrade(
			operation,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonTargetImageConflict,
			"desired Splunk image changed during the image-upgrade workflow",
			input.Now,
		)
	case operation.TargetReplicas != input.TargetReplicas:
		blockImageUpgrade(
			operation,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonReplicaConflict,
			"desired replica count changed during the image-upgrade workflow",
			input.Now,
		)
	default:
		return SHCImageUpgradeClassificationDecision{
			Classification: SHCImageUpgradeResume,
			Operation:      operation,
			Reason:         operation.Reason,
			Message:        operation.Message,
		}
	}
	return SHCImageUpgradeClassificationDecision{
		Classification: SHCImageUpgradeBlock,
		Operation:      operation,
		Reason:         operation.Reason,
		Message:        operation.Message,
	}
}

func validateImageUpgradeSourcePods(
	input SHCImageUpgradeClassificationInput,
) ([]SHCImageUpgradePod, *SHCImageUpgradeClassificationDecision) {
	pods := append([]SHCImageUpgradePod(nil), input.Pods...)
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Ordinal < pods[j].Ordinal
	})
	if len(pods) != int(input.TargetReplicas) {
		decision := imageUpgradeClassificationWithoutOperation(
			SHCImageUpgradeWait,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"wait for every expected Search Head Pod before classifying an image upgrade",
		)
		return nil, &decision
	}
	sourceImage := ""
	for ordinal, pod := range pods {
		if pod.Ordinal != int32(ordinal) || !pod.Exists || !pod.Ready ||
			pod.Deleting || pod.Image == "" {
			decision := imageUpgradeClassificationWithoutOperation(
				SHCImageUpgradeWait,
				enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
				"wait for stably ready source Search Head Pods before classifying an image upgrade",
			)
			return nil, &decision
		}
		if sourceImage == "" {
			sourceImage = pod.Image
			continue
		}
		if sourceImage != pod.Image {
			decision := imageUpgradeClassificationWithoutOperation(
				SHCImageUpgradeBlock,
				enterpriseApi.SearchHeadClusterImageUpgradeReasonMixedSourceImages,
				"source Search Head Pods have mixed images without a recorded image-upgrade workflow",
			)
			return nil, &decision
		}
	}
	return pods, nil
}

func imageUpgradeClassificationWithoutOperation(
	classification SHCImageUpgradeClassification,
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
	message string,
) SHCImageUpgradeClassificationDecision {
	return SHCImageUpgradeClassificationDecision{
		Classification: classification,
		Reason:         reason,
		Message:        message,
	}
}

func blockImageUpgrade(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
	message string,
	now time.Time,
) {
	timestamp := metav1.NewTime(now)
	operation.Phase = enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked
	operation.Reason = reason
	operation.Message = message
	operation.PhaseStartedAt = &timestamp
	operation.LastTransitionTime = &timestamp
}
