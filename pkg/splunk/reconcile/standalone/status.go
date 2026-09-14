// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package standalone

import (
	"context"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/apimachinery/pkg/types"
)

const maxRetryCountForCRStatusUpdate = 10

func updateCRStatus(ctx context.Context, client splcommon.ControllerClient, origCR *enterpriseApi.Standalone, crError *error) {
	scopedLog := logging.FromContext(ctx).With("func", "updateCRStatus", "original cr version", origCR.GetResourceVersion())

	var tryCnt int
	for tryCnt = 0; tryCnt < maxRetryCountForCRStatusUpdate; tryCnt++ {
		latestCR, err := fetchCurrentCRWithStatusUpdate(ctx, client, origCR, crError)
		if err != nil {
			if origCR.GetDeletionTimestamp() == nil {
				scopedLog.ErrorContext(ctx, "unable to Read the latest CR from the K8s", "error", err)
			}
			continue
		}

		scopedLog.InfoContext(ctx, "trying to update", "count", tryCnt)
		curCRVersion := latestCR.GetResourceVersion()
		err = client.Status().Update(ctx, latestCR)
		if err == nil {
			updatedCRVersion := latestCR.GetResourceVersion()
			scopedLog.InfoContext(ctx, "status update successful", "current CR version", curCRVersion, "updated CR version", updatedCRVersion)

			for chkCnt := 0; chkCnt < maxRetryCountForCRStatusUpdate; chkCnt++ {
				crAfterUpdate, err := fetchCurrentCRWithStatusUpdate(ctx, client, latestCR, crError)
				if err == nil && updatedCRVersion == crAfterUpdate.GetResourceVersion() {
					scopedLog.InfoContext(ctx, "cache is reflecting the latest CR", "updated CR version", updatedCRVersion)
					break
				}
				time.Sleep(time.Duration(chkCnt) * 10 * time.Millisecond)
			}
			break
		}

		scopedLog.ErrorContext(ctx, "error trying to update the CR status", "error", err)
		time.Sleep(time.Duration(tryCnt) * 10 * time.Millisecond)
	}

	if origCR.GetDeletionTimestamp() == nil && tryCnt >= maxRetryCountForCRStatusUpdate {
		scopedLog.ErrorContext(ctx, "status update failed", "attemptCount", tryCnt)
	}
}

func fetchCurrentCRWithStatusUpdate(ctx context.Context, client splcommon.ControllerClient, origCR *enterpriseApi.Standalone, crError *error) (*enterpriseApi.Standalone, error) {
	namespacedName := types.NamespacedName{Name: origCR.GetName(), Namespace: origCR.GetNamespace()}
	latestCR := &enterpriseApi.Standalone{}
	if err := client.Get(ctx, namespacedName, latestCR); err != nil {
		return nil, err
	}

	origCR.Status.Message = ""
	if crError != nil && *crError != nil {
		origCR.Status.Message = (*crError).Error()
	}
	origCR.Status.DeepCopyInto(&latestCR.Status)
	return latestCR, nil
}
