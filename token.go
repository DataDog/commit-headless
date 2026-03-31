package main

import "strings"

// isUserToken returns true if the token appears to be a user-associated token
// (PAT, OAuth, or user-to-server) based on its prefix. These tokens produce
// signed commits when used with the GraphQL API.
func isUserToken(token string) bool {
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "github_pat_"} {
		if strings.HasPrefix(token, prefix) && len(token) > len(prefix) {
			return true
		}
	}
	return false
}

type envGetter func(string) string

func getToken(getter envGetter) string {
	candidates := []string{"HEADLESS_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"}
	for _, k := range candidates {
		if v := getter(k); v != "" {
			return v
		}
	}

	return ""
}
