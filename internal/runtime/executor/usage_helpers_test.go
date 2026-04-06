package executor

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestUsageReporterPrefersBillingModelFromGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Set("billingModel", "gpt-5-4")

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	reporter := buildUsageReporterForTest(ctx, "openai", "gpt-5.4")
	record := reporter.buildRecord(usage.Detail{}, false)

	if record.Model != "gpt-5-4" {
		t.Fatalf("record.Model = %q, want %q", record.Model, "gpt-5-4")
	}
}
