package executor

import (
	"testing"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

func TestIsModerationBlock(t *testing.T) {
	patterns := authpkg.DefaultModerationPatterns
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"code", 400, `{"error":{"message":"Your request was rejected.","type":"invalid_request_error","code":"moderation_blocked"}}`, true},
		{"type", 400, `{"error":{"message":"nope","type":"image_generation_user_error"}}`, true},
		{"pattern", 400, `{"error":{"message":"Rejected by our Safety System","type":"invalid_request_error"}}`, true},
		{"wrapped upstream body", 400, `{"error":{"message":"{\"error\":{\"code\":\"moderation_blocked\",\"message\":\"blocked\"}}"}}`, true},
		{"plain string error", 400, `{"error":"violates the content policy"}`, true},
		{"status lost", 0, `{"error":{"code":"moderation_blocked"}}`, true},
		{"venice invalid request", 400, `{"error":{"message":"venice image failed: Invalid request"}}`, false},
		{"429 with pattern", 429, `{"error":{"message":"moderation queue full"}}`, false},
		{"500 with code", 500, `{"error":{"code":"moderation_blocked"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isModerationBlock(tt.status, []byte(tt.body), patterns); got != tt.want {
				t.Fatalf("isModerationBlock = %v, want %v", got, tt.want)
			}
		})
	}
	if isModerationBlock(400, []byte(`{"error":{"message":"safety system"}}`), nil) {
		t.Fatal("an empty pattern list must not match messages")
	}
}

func TestDecide(t *testing.T) {
	base := authpkg.VirtualModel{
		FallbackOnStatus:   []int{429, 500, 502, 503, 504, 400},
		NoFallbackOnStatus: []int{503},
		ModerationPatterns: authpkg.DefaultModerationPatterns,
	}
	moderation := Response{Status: 400, Body: []byte(`{"error":{"code":"moderation_blocked"}}`)}
	tests := []struct {
		name     string
		modOn    bool
		a        attempt
		fallback bool
	}{
		{"timeout", false, attempt{timeout: true}, true},
		{"transport", false, attempt{transportErr: errTest}, true},
		{"listed status", false, attempt{resp: Response{Status: 500}}, true},
		{"no_fallback wins", false, attempt{resp: Response{Status: 503}}, false},
		{"unlisted status", false, attempt{resp: Response{Status: 401}}, false},
		// 400 is listed above, yet a blocked prompt follows the switch alone.
		{"moderation off", false, attempt{resp: moderation}, false},
		{"moderation on", true, attempt{resp: moderation}, true},
		{"lost status counts as 502", false, attempt{statusLost: true, resp: Response{Status: 200, Body: []byte(`{"error":{"message":"boom"}}`)}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := base
			model.FallbackOnModeration = tt.modOn
			if got := decide(model, tt.a); got.fallback != tt.fallback {
				t.Fatalf("fallback = %v (%s), want %v", got.fallback, got.reason, tt.fallback)
			}
		})
	}
}

type testError string

func (e testError) Error() string { return string(e) }

const errTest = testError("connection refused")
