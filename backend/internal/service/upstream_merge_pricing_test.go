package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergedRecordUsageSeparatesImageCacheFromImageInput(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-custom": {
			InputCostPerToken: 5e-6, OutputCostPerToken: 30e-6,
			InputCostPerImageToken: 8e-6, CacheReadInputTokenCost: 1.25e-6,
			CacheReadInputImageTokenCost: 2e-6,
		},
	}})
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "merged-image-cache", Model: "gpt-custom",
			Usage: OpenAIUsage{InputTokens: 100, ImageInputTokens: 80, CacheReadInputTokens: 50, ImageCacheReadTokens: 40},
		},
		APIKey: &APIKey{ID: 1001}, User: &User{ID: 2001}, Account: &Account{ID: 3001},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 10*5e-6, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, 40*8e-6, usageRepo.lastLog.ImageInputCost, 1e-12)
	require.InDelta(t, 10*1.25e-6+40*2e-6, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, 10*5e-6+40*8e-6+10*1.25e-6+40*2e-6, usageRepo.lastLog.TotalCost, 1e-12)
}

func TestPriorityLongContextTierUsesSelectedThresholdAndExplicitOverrides(t *testing.T) {
	for _, tc := range []struct {
		name          string
		override      string
		wantThreshold int
		wantInput     float64
		wantOutput    float64
	}{
		{"selected threshold", "", 128000, 2, 1.5},
		{"explicit priority disabled", `,"long_context_input_cost_multiplier_priority":0,"long_context_output_cost_multiplier_priority":1`, 128000, 0, 1},
		{"explicit standard disabled", `,"long_context_input_token_threshold":0`, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := (&PricingService{}).parsePricingData([]byte(`{"gpt-custom":{
    "input_cost_per_token":0.000001,"output_cost_per_token":0.000004,
    "input_cost_per_token_priority":0.000002,"output_cost_per_token_priority":0.000008,
    "input_cost_per_token_above_128k_tokens":0.000002,"output_cost_per_token_above_128k_tokens":0.000006,
    "input_cost_per_token_above_128k_tokens_priority":0.000004,"output_cost_per_token_above_128k_tokens_priority":0.000012,
    "input_cost_per_token_above_272k_tokens":0.000003,"output_cost_per_token_above_272k_tokens":0.000008,
    "input_cost_per_token_above_272k_tokens_priority":0.000006,"output_cost_per_token_above_272k_tokens_priority":0.000016` + tc.override + `}}`))
			require.NoError(t, err)
			pricing := data["gpt-custom"]
			require.Equal(t, tc.wantThreshold, pricing.LongContextInputTokenThreshold)
			require.InDelta(t, tc.wantInput, pricing.LongContextInputCostMultiplierPriority, 1e-12)
			require.InDelta(t, tc.wantOutput, pricing.LongContextOutputCostMultiplierPriority, 1e-12)
		})
	}
}

func TestAccountStatsCombinesImageTokensAndHourlyCachePricing(t *testing.T) {
	input, imageInput, output, imageOutput := 1e-6, 2e-6, 3e-6, 4e-6
	cache5m, cache1h := 5e-6, 6e-6
	cost := calculateTokenStatsCost(&ChannelModelPricing{
		InputPrice: &input, ImageInputPrice: &imageInput, OutputPrice: &output,
		ImageOutputPrice: &imageOutput, CacheWritePrice: &cache5m, CacheWrite1hPrice: &cache1h,
	}, UsageTokens{
		InputTokens: 100, ImageInputTokens: 20, OutputTokens: 50, ImageOutputTokens: 10,
		CacheCreationTokens: 30, CacheCreation5mTokens: 10, CacheCreation1hTokens: 20,
	})
	require.NotNil(t, cost)
	require.InDelta(t, 80*input+20*imageInput+40*output+10*imageOutput+10*cache5m+20*cache1h, *cost, 1e-12)
}
