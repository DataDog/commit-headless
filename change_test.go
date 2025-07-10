package main

import (
	"strings"
	"testing"
)

func TestChangeBody(t *testing.T) {
	testcases := []struct {
		input    string
		author   string
		trailers []string

		headline string
		body     string
	}{{
		"subject\n\nbody\n\nco-authored-by: author", "author", nil,
		"subject", "body\n\nco-authored-by: author",
	}, {
		"subject only", "", nil,
		"subject only", "",
	}, {
		"no trailers and no author\n\nbody", "", nil,
		"no trailers and no author", "body",
	}, {
		"no trailers with author", "author", nil,
		"no trailers with author", "Co-authored-by: author",
	}, {
		"no trailers with author and body\n\nbody", "author", nil,
		"no trailers with author and body", "body\n\nCo-authored-by: author",
	}, {
		// if the first line looks like a trailer, it's not a trailer
		"Co-authored-by: subject", "author", nil,
		"Co-authored-by: subject", "Co-authored-by: author",
	}, {
		"subject\n\nbody", "author",
		[]string{"Foo: bar"},
		"subject", "body\n\nCo-authored-by: author\nFoo: bar",
	}}

	for _, tc := range testcases {
		t.Run("", func(t *testing.T) {
			change := Change{author: tc.author, message: tc.input, trailers: tc.trailers}
			headline, body := change.Headline(), change.Body()

			if headline != tc.headline {
				t.Logf("wrong headline, got=%s, want=%s", headline, tc.headline)
				t.Fail()
			}

			tc.body = strings.TrimSpace(tc.body)

			if body != tc.body {
				t.Logf("wrong body, got=%q, want=%q", body, tc.body)
				t.Fail()
			}
		})
	}
}
