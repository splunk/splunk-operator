package secondary

import (
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/business/core/types/constants"
)

type Provisioner interface {
	PrepareSpec()
	Build() error
	Await() error
	State() (pgcConstants.State, pgcConstants.Reason, error)
}
