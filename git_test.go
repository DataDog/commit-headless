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

func TestEmptyFileVsDeletedFile(t *testing.T) {
	testcases := []struct {
		name      string
		content   []byte
		isDeleted bool
	}{
		{
			name:      "nil content means deleted",
			content:   nil,
			isDeleted: true,
		},
		{
			name:      "empty byte slice means empty file (not deleted)",
			content:   []byte{},
			isDeleted: false,
		},
		{
			name:      "file with content is not deleted",
			content:   []byte("hello world"),
			isDeleted: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			change := Change{
				hash:    "deadbeef",
				author:  "Test Author <test@example.com>",
				message: "test commit",
				entries: map[string][]byte{
					"testfile.txt": tc.content,
				},
			}

			// Simulate what github.go does to determine if a file is deleted
			isDeleted := change.entries["testfile.txt"] == nil

			if isDeleted != tc.isDeleted {
				t.Errorf("expected isDeleted=%v, got=%v for content=%v", tc.isDeleted, isDeleted, tc.content)
			}

			// Verify that empty files are distinguishable from deleted files
			if tc.isDeleted {
				if change.entries["testfile.txt"] != nil {
					t.Error("deleted files should have nil content")
				}
			} else if tc.content != nil && len(tc.content) == 0 {
				// This is an empty file
				if change.entries["testfile.txt"] == nil {
					t.Error("empty files should not have nil content")
				}
				if len(change.entries["testfile.txt"]) != 0 {
					t.Error("empty files should have zero-length content")
				}
			}
		})
	}
}
