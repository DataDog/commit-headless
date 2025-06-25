package main

import (
	"bytes"
	"io"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"
)

func TestCommitsFromStdin(t *testing.T) {
	fakefs := fstest.MapFS{
		"unpiped": &fstest.MapFile{},
		"piped": &fstest.MapFile{
			Data: []byte("aaaa\nbbbb"),
			Mode: fs.FileMode(0) | fs.ModeNamedPipe,
		},
	}

	fakefile := func(name string) fs.File {
		t.Helper()
		out, err := fakefs.Open(name)
		if err != nil {
			t.Error(err)
			t.FailNow()
		}
		return out
	}

	testcases := []struct {
		name    string
		in      io.Reader
		want    []string
		wantErr error
	}{{
		name: "default",
		in:   bytes.NewBufferString("deadbeef\nabracadabra"),
		want: []string{"abracadabra", "deadbeef"},
	}, {
		name: "mixed input",
		in:   bytes.NewBufferString("deadbeef\nnot-a-commit-hash\nabracadabra"),
		want: []string{"abracadabra", "deadbeef"},
	}, {
		name: "stuff after the hash",
		in:   bytes.NewBufferString("deadbeef (test-branch) feat: hello\nnot-a-commit-hash\nabracadabra"),
		want: []string{"abracadabra", "deadbeef"},
	}, {
		name:    "not a pipe",
		in:      fakefile("unpiped"),
		wantErr: errUnpipedStdin,
	}, {
		name: "piped",
		in:   fakefile("piped"),
		want: []string{"bbbb", "aaaa"},
	}, {
		name:    "no commits",
		in:      bytes.NewBufferString("not\na\ncommit"),
		wantErr: errNoCommitsStdin,
	}, {
		name:    "only spaces",
		in:      bytes.NewBufferString("  "),
		wantErr: errNoCommitsStdin,
	}, {
		name: "blank lines",
		in:   bytes.NewBufferString("abcd\n  \ndead"),
		want: []string{"dead", "abcd"},
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := commitsFromStdin(tc.in)
			if err != tc.wantErr {
				t.Fatalf("expected error; got=%v want=%v", err, tc.wantErr)
			}

			if !slices.Equal(got, tc.want) {
				t.Fatalf("expected equal hashes; got=%v want=%v", got, tc.want)
			}
		})
	}
}
