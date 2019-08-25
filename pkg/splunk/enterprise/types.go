// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package enterprise

// SplunkInstanceType is used to represent the type of Splunk instance (search head, indexer, etc).
type SplunkInstanceType string

const SPLUNK_STANDALONE SplunkInstanceType = "standalone"
const SPLUNK_CLUSTER_MASTER SplunkInstanceType = "cluster-master"
const SPLUNK_SEARCH_HEAD SplunkInstanceType = "search-head"
const SPLUNK_INDEXER SplunkInstanceType = "indexer"
const SPLUNK_DEPLOYER SplunkInstanceType = "deployer"
const SPLUNK_LICENSE_MASTER SplunkInstanceType = "license-master"

func (instanceType SplunkInstanceType) ToString() string {
	return string(instanceType)
}
