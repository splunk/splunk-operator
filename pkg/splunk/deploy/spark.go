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

package deploy

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/types"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ReconcileSpark reconciles the Deployments and Services for a Spark cluster.
func ReconcileSpark(client ControllerClient, cr *enterprisev1.Spark) error {

	// validate and updates defaults for CR
	err := spark.ValidateSparkSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
		return err
	}

	// create or update a service for spark master
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkMaster, false))
	if err != nil {
		return err
	}

	// create or update a headless service for spark workers
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkWorker, true))
	if err != nil {
		return err
	}

	// create or update deployment for spark master
	deployment, err := spark.GetSparkDeployment(cr, spark.SparkMaster)
	if err != nil {
		return err
	}
	err = ApplyDeployment(client, deployment)
	if err != nil {
		return err
	}

	// create or update deployment for spark worker
	deployment, err = spark.GetSparkDeployment(cr, spark.SparkWorker)
	if err != nil {
		return err
	}
	err = ApplyDeployment(client, deployment)
	if err != nil {
		return err
	}

	return nil
}

// applySpark creates or updates a Splunk Enterprise Spark custom resource definition (CRD).
func applySpark(client ControllerClient, cr *enterprisev1.SplunkEnterprise) error {
	scopedLog := log.WithName("applySpark").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	revised, err := enterprise.GetSparkResource(cr)
	if err != nil {
		return err
	}
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	var current enterprisev1.Spark

	err = client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing Spark
		if !reflect.DeepEqual(current.Spec, revised.Spec) {
			current.Spec = revised.Spec
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for Spark")
		}
	} else {
		err = CreateResource(client, revised)
	}

	return err
}
