package main

import (
	"strings"
	"testing"
)

func requireNoError(t *testing.T, err error, msg ...any) {
	t.Helper()

	if err == nil {
		return
	}

	if len(msg) == 0 {
		t.Fatalf("expected no error, got: %s", err.Error())
		return
	}

	if len(msg) == 1 {
		t.Fatal(msg[0].(string))
		return
	}

	t.Fatalf(msg[0].(string), msg[1:]...)
}

func TestCommitHashes(t *testing.T) {
	testcases := []struct {
		input string
		ok    bool
	}{{
		"deadbeef", true,
	}, {
		"HEAD", false,
	}, {
		"f8034fe40034a602c232b8cbe06ab79e518f71c1", true,
	}, {
		"fee", false,
	}, {
		"f", false,
	}, {
		"", false,
	}, {
		strings.Repeat("a", 40), true,
	}, {
		strings.Repeat("a", 41), false,
	}}

	for _, tc := range testcases {
		t.Run(tc.input, func(t *testing.T) {
			want := tc.ok
			got := hashRegex.MatchString(tc.input)
			if want != got {
				t.Fatalf("commit hash check mismatch; got=%t, want=%t", got, want)
			}
		})
	}
}
