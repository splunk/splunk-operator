package resources


type SplunkServiceType string

const SERVICE SplunkServiceType = "service"
const HEADLESS_SERVICE SplunkServiceType = "headless"

func (s SplunkServiceType) ToString() string {
	return string(s)
}