package management

import (
	"testing"

	authpkg "github.com/trungking/cpa-plugin-venice/internal/auth"
)

func TestQuotaResponseMapsVeniceUsage(t *testing.T) {
	storage, err := authpkg.ParseStorage([]byte(`{"type":"venice","cookie":"__client=secret","account_plan":"PRO","quota":{"bundledCreditsUsage":{"availableCredits":25,"tierCap":100,"nextRefillAt":1784460283868},"rateLimits":{"conversation":{"remaining":1800,"resetAt":1784460283868}}}}`))
	if err != nil || storage == nil {
		t.Fatalf("ParseStorage = %#v, %v", storage, err)
	}
	resp := quotaResponse(*storage)
	if resp.Subscription == nil || resp.Subscription.Plan != "PRO" {
		t.Fatalf("subscription = %#v", resp.Subscription)
	}
	if len(resp.Summary) != 1 || resp.Summary[0].Key != "credits" || resp.Summary[0].Value != 25 {
		t.Fatalf("summary = %#v", resp.Summary)
	}
	if len(resp.Groups) != 1 || len(resp.Groups[0].Buckets) != 2 {
		t.Fatalf("groups = %#v", resp.Groups)
	}
	credits, conversation := resp.Groups[0].Buckets[0], resp.Groups[0].Buckets[1]
	if credits.Window != "credits" || credits.RemainingFraction != 0.25 || credits.ResetTime != "2026-07-19T11:24:43Z" {
		t.Fatalf("credits bucket = %#v", credits)
	}
	if conversation.Window != "conversation" || conversation.RemainingFraction != 0.5 {
		t.Fatalf("conversation bucket = %#v", conversation)
	}
}
