package main

import (
	"slices"
	"testing"
)

func TestSplitChange(t *testing.T) {
	change := Change{
		entries: map[string][]byte{
			"a-file":  []byte("hello world"),
			"b-empty": {},
			"deleted": nil,
		},
	}

	added, deleted := (&Client{}).splitChange(change)

	if len(added) != 2 {
		t.Errorf("expected 2 added changes, got %d", len(added))
	} else {
		additions := []string{added[0].Path, added[1].Path}
		slices.Sort(additions)

		if additions[0] != "a-file" || additions[1] != "b-empty" {
			t.Errorf("expected 'a-file' and 'b-empty' to be added, but got %q", additions)
		}
	}

	if len(deleted) != 1 {
		t.Errorf("expected 1 deleted change, got %d", len(deleted))
	} else if deleted[0].Path != "deleted" {
		t.Errorf("expected deleted[0].Path to be 'deleted', got %s", deleted[0].Path)
	}
}
