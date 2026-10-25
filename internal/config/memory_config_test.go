package config

import "testing"

func TestMemoryPreambleBudgetDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Memory.PreambleTokens != 1500 || cfg.Memory.PreambleSummaries != 3 {
		t.Fatalf("defaults = tokens %d summaries %d, want 1500 and 3", cfg.Memory.PreambleTokens, cfg.Memory.PreambleSummaries)
	}
	if cfg.Memory.Path != ".groot/groot.db" {
		t.Fatalf("memory path default changed to %q", cfg.Memory.Path)
	}
}

func TestMemoryPreambleBudgetReadsFromFile(t *testing.T) {
	p := writeFile(t, "groot.yaml", `
memory:
  preamble_tokens: 800
  preamble_summaries: 2
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory.PreambleTokens != 800 || cfg.Memory.PreambleSummaries != 2 {
		t.Fatalf("got tokens %d summaries %d, want 800 and 2", cfg.Memory.PreambleTokens, cfg.Memory.PreambleSummaries)
	}
	if cfg.Memory.Path != ".groot/groot.db" {
		t.Fatalf("a file that sets only the budget must keep the default path, got %q", cfg.Memory.Path)
	}
}

func TestMemoryPreambleBudgetValidation(t *testing.T) {
	tests := []struct {
		name    string
		tokens  int
		summary int
		wantErr bool
	}{
		{name: "defaults are valid", tokens: 1500, summary: 3},
		{name: "zero tokens is rejected", tokens: 0, summary: 3, wantErr: true},
		{name: "negative summaries are rejected", tokens: 1500, summary: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Memory.PreambleTokens = tt.tokens
			cfg.Memory.PreambleSummaries = tt.summary
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
