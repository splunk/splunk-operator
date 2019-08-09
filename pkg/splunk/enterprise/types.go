package enterprise


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
