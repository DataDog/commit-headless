package main

import "testing"

func TestIsUserToken(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"ghp_abc123", true},
		{"gho_abc123", true},
		{"ghu_abc123", true},
		{"github_pat_abc123", true},
		{"ghs_abc123", false},
		{"ghr_abc123", false},
		{"some-random-token", false},
		{"", false},
		{"ghp_", false}, // prefix only, no actual token content
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			if got := isUserToken(tt.token); got != tt.want {
				t.Errorf("isUserToken(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}
