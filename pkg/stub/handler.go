package stub

import (
	"context"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
)


func NewHandler() sdk.Handler {
	return &Handler{}
}


type Handler struct {
	// Fill me
}


func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
		case *v1alpha1.SplunkInstance:

			err := ValidateSplunkCustomResource(o)
			if err != nil {
				return err
			}

			if event.Deleted {
				return CleanupLaunch(o)
			}

			err = LaunchStandalones(o)
			if err != nil {
				return err
			}

			err = LaunchCluster(o)
			if err != nil {
				return err
			}

			err = UpdateStandalones(o)
			if err != nil {
				return err
			}

			err = UpdateCluster(o)
			if err != nil {
				return err
			}
		}
	return nil
}