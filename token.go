package main

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
