package billing

import (
	"context"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func init() {
	coreusage.RegisterPlugin(&usagePlugin{})
}

type usagePlugin struct{}

func (p *usagePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	DefaultManager().HandleUsageRecord(ctx, record)
}
