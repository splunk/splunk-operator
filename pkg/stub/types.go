package stub


type SplunkInstanceType string

const SPLUNK_STANDALONE SplunkInstanceType = "standalone"
const SPLUNK_MASTER SplunkInstanceType = "master"
const SPLUNK_SEARCH_HEAD SplunkInstanceType = "search-head"
const SPLUNK_INDEXER SplunkInstanceType = "indexer"

func (instanceType SplunkInstanceType) toString() string {
	return string(instanceType)
}


type ServiceType string

const HEADLESS_SERVICE ServiceType = "headless"
const EXPOSE_STANDALONE_SERVICE ServiceType = "expose-standalone"
const EXPOSE_MASTER_SERVICE ServiceType = "expose-master"

func (s ServiceType) toString() string {
	return string(s)
}