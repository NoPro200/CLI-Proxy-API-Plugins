package management

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	authpkg "github.com/trungking/cpa-plugin-venice/internal/auth"
)

// The accounts page and the host quota API share these upstream limits.
const (
	conversationLimitCap = 3600
	imageLimitCap        = 1000
)

// FetchQuota answers the host quota API, which the management center renders
// on its quota and auth file pages.
func (p *Provider) FetchQuota(ctx context.Context, req pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, error) {
	storage, err := authpkg.ParseStorage(req.StorageJSON)
	if err != nil {
		return pluginapi.QuotaFetchResponse{}, err
	}
	if storage == nil {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("venice auth storage is missing")
	}
	if req.HTTPClient != nil && storage.Cookie != "" {
		// Without a fresh session, the quota recorded at the last refresh is still worth showing.
		if errRefresh := authpkg.RefreshStorage(ctx, req.HTTPClient, storage); errRefresh != nil && len(storage.Quota) == 0 {
			return pluginapi.QuotaFetchResponse{}, errRefresh
		}
	}
	return quotaResponse(*storage), nil
}

func quotaResponse(storage authpkg.Storage) pluginapi.QuotaFetchResponse {
	var resp pluginapi.QuotaFetchResponse
	if storage.AccountPlan != "" {
		resp.Subscription = &pluginapi.QuotaSubscription{Plan: storage.AccountPlan}
	}
	creditCap := quotaMetric(storage.Quota, []string{"bundledCreditsUsage", "tierCap"}, []string{"bundledCreditsUsage", "monthlyRefillCredits"})
	credits := creditAvailable(storage.Quota, creditCap)
	if credits > 0 || creditCap > 0 {
		resp.Summary = []pluginapi.QuotaMetric{{Key: "credits", Label: "Credits", Value: credits, Format: "number"}}
	}
	var buckets []pluginapi.QuotaBucket
	if creditCap > 0 {
		buckets = append(buckets, quotaBucket("credits", credits, creditCap, nestedValueOrNil(storage.Quota, []string{"bundledCreditsUsage", "nextRefillAt"})))
	}
	for _, limit := range []struct {
		window, key string
		cap         float64
	}{{"conversation", "conversation", conversationLimitCap}, {"images", "image", imageLimitCap}} {
		remainingPath := []string{"rateLimits", limit.key, "remaining"}
		if _, ok := nestedValue(storage.Quota, remainingPath); !ok {
			continue
		}
		buckets = append(buckets, quotaBucket(limit.window, quotaMetric(storage.Quota, remainingPath), limit.cap, nestedValueOrNil(storage.Quota, []string{"rateLimits", limit.key, "resetAt"})))
	}
	if len(buckets) > 0 {
		resp.Groups = []pluginapi.QuotaGroup{{DisplayName: "Venice", Buckets: buckets}}
	}
	return resp
}

func quotaBucket(window string, remaining float64, cap float64, resetAt any) pluginapi.QuotaBucket {
	bucket := pluginapi.QuotaBucket{
		Window:            window,
		RemainingFraction: min(max(remaining/cap, 0), 1),
		Description:       fmt.Sprintf("%.0f of %.0f left", remaining, cap),
	}
	if at, ok := epochMillis(resetAt); ok {
		bucket.ResetTime = at.Format(time.RFC3339)
	}
	return bucket
}
