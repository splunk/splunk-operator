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

package indexercluster

import (
	"context"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
)

const maxRetryCountForCRStatusUpdate = 10

// updateCRStatus fetches the latest CR, and on top of that, updates latest status including error messages as well
func updateCRStatus(ctx context.Context, client splcommon.ControllerClient, origCR *enterpriseApi.IndexerCluster, crError *error) {
	scopedLog := logging.FromContext(ctx).With("func", "updateCRStatus", "original cr version", origCR.GetResourceVersion())

	var tryCnt int
	for tryCnt = 0; tryCnt < maxRetryCountForCRStatusUpdate; tryCnt++ {
		latestCR, err := k8sops.GetCurrentCRWithStatusUpdate(ctx, client, origCR, crError)
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

			// While the current reconcile is in progress, there may be new event(s) from the
			// list of watchers satisfying the predicates. That triggeres a new reconcile right after
			// exiting from the current reconcile, in which case, refers the cached version of the
			// CR missing the updates we are doing here. From K8s resource point of view, this
			// may not be an issue(i.e., expectation is always to be declarative), but the  application
			// specific status may not be idempotent(example. trying to install an app which was already installed).
			// So, always make sure that the cache is reflecting the latest CR, before the next event
			// waiting in the Q triggers the next reconcile
			for chkCnt := 0; chkCnt < maxRetryCountForCRStatusUpdate; chkCnt++ {
				crAfterUpdate, err := k8sops.GetCurrentCRWithStatusUpdate(ctx, client, latestCR, crError)
				if err == nil && updatedCRVersion == crAfterUpdate.GetResourceVersion() {
					scopedLog.InfoContext(ctx, "cache is reflecting the latest CR", "updated CR version", updatedCRVersion)
					// Latest CR is reflecting in the cache
					break
				}
				time.Sleep(time.Duration(chkCnt) * 10 * time.Millisecond)
			}
			// Status update successful
			break
		}

		scopedLog.ErrorContext(ctx, "error trying to update the CR status", "error", err)
		time.Sleep(time.Duration(tryCnt) * 10 * time.Millisecond)
	}

	if origCR.GetDeletionTimestamp() == nil && tryCnt >= maxRetryCountForCRStatusUpdate {
		scopedLog.ErrorContext(ctx, "status update failed", "attemptCount", tryCnt)
	}
}
