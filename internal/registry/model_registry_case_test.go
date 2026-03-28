package registry

import "testing"

// TestGetModelProvidersCaseInsensitive validates that provider lookup succeeds when the
// incoming request model ID differs only in case from the registered model ID.
//
// Root cause addressed: Codex/OpenAI providers register models with display-case IDs
// (e.g. "GPT-5.4") derived from the alias field of their config. Requests that use
// canonical lowercase IDs (e.g. "gpt-5.4") must still resolve to the correct provider.
func TestGetModelProvidersCaseInsensitive(t *testing.T) {
	tests := []struct {
		name            string
		registeredID    string
		requestedID     string
		expectProviders []string
	}{
		{
			name:            "exact match still works",
			registeredID:    "gpt-5.4",
			requestedID:     "gpt-5.4",
			expectProviders: []string{"codex"},
		},
		{
			name:            "lowercase request matches uppercase registered ID",
			registeredID:    "GPT-5.4",
			requestedID:     "gpt-5.4",
			expectProviders: []string{"codex"},
		},
		{
			name:            "uppercase request matches lowercase registered ID",
			registeredID:    "gpt-5",
			requestedID:     "GPT-5",
			expectProviders: []string{"codex"},
		},
		{
			name:            "mixed-case display name matches canonical request",
			registeredID:    "GPT-5.3 Codex",
			requestedID:     "gpt-5.3 codex",
			expectProviders: []string{"codex"},
		},
		{
			name:            "completely unknown model returns no providers",
			registeredID:    "GPT-5.4",
			requestedID:     "gpt-99-unknown",
			expectProviders: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestModelRegistry()
			r.RegisterClient("client-codex", "codex", []*ModelInfo{
				{ID: tt.registeredID, Object: "model", OwnedBy: "openai"},
			})

			got := r.GetModelProviders(tt.requestedID)

			if len(tt.expectProviders) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no providers for %q, got %v", tt.requestedID, got)
				}
				return
			}
			if len(got) != len(tt.expectProviders) {
				t.Fatalf("expected providers %v for %q, got %v", tt.expectProviders, tt.requestedID, got)
			}
			for i, p := range tt.expectProviders {
				if got[i] != p {
					t.Errorf("expected provider[%d]=%q, got %q", i, p, got[i])
				}
			}
		})
	}
}

// TestGetModelInfoCaseInsensitive validates that model info lookup is case-insensitive.
func TestGetModelInfoCaseInsensitive(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-codex", "codex", []*ModelInfo{
		{ID: "GPT-5.4", Object: "model", OwnedBy: "openai", DisplayName: "GPT-5.4"},
	})

	// Exact match
	info := r.GetModelInfo("GPT-5.4", "codex")
	if info == nil {
		t.Fatal("expected model info for exact match GPT-5.4, got nil")
	}

	// Case-insensitive match: lowercase request should find the display-case registration
	info = r.GetModelInfo("gpt-5.4", "codex")
	if info == nil {
		t.Fatal("expected model info for case-insensitive match gpt-5.4, got nil")
	}
	if info.ID != "GPT-5.4" {
		t.Errorf("expected info.ID=GPT-5.4, got %q", info.ID)
	}
}

// TestGetModelProvidersCaseInsensitiveAfterUnregister verifies that the lowercase index
// is cleaned up correctly after a client is unregistered, preventing stale lookups.
func TestGetModelProvidersCaseInsensitiveAfterUnregister(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "codex", []*ModelInfo{
		{ID: "GPT-5.4", Object: "model", OwnedBy: "openai"},
	})

	// Should find the model before unregistering
	if providers := r.GetModelProviders("gpt-5.4"); len(providers) == 0 {
		t.Fatal("expected to find providers before unregister")
	}

	r.UnregisterClient("client-1")

	// Should not find the model after unregistering
	if providers := r.GetModelProviders("gpt-5.4"); len(providers) != 0 {
		t.Fatalf("expected no providers after unregister, got %v", providers)
	}
	if providers := r.GetModelProviders("GPT-5.4"); len(providers) != 0 {
		t.Fatalf("expected no providers after unregister (exact case), got %v", providers)
	}
}

// TestGetModelProviders_BothEndpoints ensures that both /v1/chat/completions and
// /v1/responses share the same underlying model-to-provider resolution path.
// This test simulates the scenario described in the bug report where gpt-5.4 fails
// despite GPT-5.4 being present in /v1/models.
func TestGetModelProviders_BothEndpoints(t *testing.T) {
	r := newTestModelRegistry()

	// Simulate Codex config-key registration: alias="GPT-5.4" is the model ID
	r.RegisterClient("codex-apikey-1", "codex", []*ModelInfo{
		{ID: "GPT-5.4", Object: "model", OwnedBy: "openai"},
		{ID: "GPT-5", Object: "model", OwnedBy: "openai"},
		{ID: "GPT-5.1", Object: "model", OwnedBy: "openai"},
	})

	// Both lowercase and display-case must resolve
	canonicalCases := []struct{ request, display string }{
		{"gpt-5.4", "GPT-5.4"},
		{"gpt-5", "GPT-5"},
		{"gpt-5.1", "GPT-5.1"},
	}

	for _, tc := range canonicalCases {
		t.Run(tc.request+"_canonical", func(t *testing.T) {
			if p := r.GetModelProviders(tc.request); len(p) == 0 {
				t.Errorf("canonical ID %q not routed; registered as %q", tc.request, tc.display)
			}
		})
		t.Run(tc.display+"_display", func(t *testing.T) {
			if p := r.GetModelProviders(tc.display); len(p) == 0 {
				t.Errorf("display ID %q not routed", tc.display)
			}
		})
	}

	// Truly unknown model must still fail
	if p := r.GetModelProviders("gpt-99-totally-unknown"); len(p) != 0 {
		t.Errorf("expected no providers for unknown model, got %v", p)
	}
}
