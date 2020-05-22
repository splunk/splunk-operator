// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package reconcile

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySpark reconciles the Deployments and Services for a Spark cluster.
func ApplySpark(client ControllerClient, cr *enterprisev1.Spark) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	// validate and updates defaults for CR
	err := spark.ValidateSparkSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterprisev1.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-spark-worker", cr.GetIdentifier())
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := CheckSplunkDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterprisev1.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a service for spark master
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkMaster, false))
	if err != nil {
		return result, err
	}

	// create or update a headless service for spark workers
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkWorker, true))
	if err != nil {
		return result, err
	}

	// create or update deployment for spark master
	deployment, err := spark.GetSparkDeployment(cr, spark.SparkMaster)
	if err != nil {
		return result, err
	}
	cr.Status.MasterPhase, err = ApplyDeployment(client, deployment)
	if err != nil {
		cr.Status.MasterPhase = enterprisev1.PhaseError
		return result, err
	}

	// create or update deployment for spark worker
	deployment, err = spark.GetSparkDeployment(cr, spark.SparkWorker)
	if err != nil {
		return result, err
	}
	cr.Status.Phase, err = ApplyDeployment(client, deployment)
	cr.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	if err != nil {
		cr.Status.Phase = enterprisev1.PhaseError
	} else if cr.Status.Phase == enterprisev1.PhaseReady {
		result.Requeue = false
	}
	return result, err
}
