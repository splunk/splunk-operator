// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
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

package spark

// InstanceType is used to represent the type of spark instance (master or worker).
type InstanceType string

const (
	// SparkMaster is the master node of a spark cluster
	SparkMaster InstanceType = "spark-master"

	// SparkWorker is a worker node in a spark cluster
	SparkWorker InstanceType = "spark-worker"
)

// ToString returns a string for a given InstanceType
func (instanceType InstanceType) ToString() string {
	return string(instanceType)
}
