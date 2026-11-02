package provider

import (
	"context"
	"errors"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
)

type ExternalPushProvider struct{}

func (*ExternalPushProvider) Ident() string { return models.ExternalPushType }
func (*ExternalPushProvider) Check(c *models.NotifyChannelConfig) error {
	if c.RequestType != models.ExternalPushType {
		return errors.New("external push requires its dedicated request type")
	}
	if !c.Enable {
		return nil
	} // An incomplete, disabled draft cannot send anything.
	if c.RequestConfig == nil {
		return errors.New("external push configuration is missing")
	}
	return externalpush.ValidateConfig(c.RequestConfig.ExternalPushConfig)
}
func (*ExternalPushProvider) Notify(context.Context, *NotifyRequest) *NotifyResult {
	return &NotifyResult{Err: errors.New("external push must be registered through a saved notification rule; direct test-send is disabled")}
}
