package id

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		got, err := New()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != Length {
			t.Fatalf("len(%q) = %d, want %d", got, len(got), Length)
		}
		for _, c := range got {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("id %q contains %q outside alphabet", got, c)
			}
		}
		if _, dup := seen[got]; dup {
			t.Fatalf("duplicate id %q in 1000 draws", got)
		}
		seen[got] = struct{}{}
	}
}
