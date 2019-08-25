// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package spark

// SparkInstanceType is used to represent the type of spark instance (master or worker).
type SparkInstanceType string

const SPARK_MASTER SparkInstanceType = "spark-master"
const SPARK_WORKER SparkInstanceType = "spark-worker"

func (instanceType SparkInstanceType) ToString() string {
	return string(instanceType)
}
