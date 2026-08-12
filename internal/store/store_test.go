package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileKey(t *testing.T) {
	if got := FileKey("w12"); got != "w12" {
		t.Fatalf("safe key = %q", got)
	}
	bad := FileKey("../escape")
	if strings.Contains(bad, "/") || bad == FileKey("other unsafe") {
		t.Fatalf("unsafe key = %q", bad)
	}
}

func TestOpenRequiresWorkspace(t *testing.T) {
	if _, err := Open(t.TempDir(), ""); err == nil {
		t.Fatal("expected missing workspace error")
	}
}

func TestSaveLoadPlainMarkdown(t *testing.T) {
	s, err := Open(t.TempDir(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Scratch\n\n- [ ] ship\n"
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || string(data) != want {
		t.Fatalf("canonical: %q, %v", data, err)
	}
	got, err := s.Load()
	if err != nil || got != want {
		t.Fatalf("load: %q, %v", got, err)
	}
}

func TestMigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "w7")
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "w7.json")
	if err := os.WriteFile(legacy, []byte(`{"text":"# old\n","mode":"edit"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got != "# old\n" {
		t.Fatalf("migration: %q, %v", got, err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatalf("legacy backup: %v", err)
	}
	data, _ := os.ReadFile(s.Path)
	if string(data) != got {
		t.Fatalf("canonical isn't Markdown: %q", data)
	}
}

func TestCanonicalWinsOverLegacy(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, "w1")
	if err := s.Save(""); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "w1.json"), []byte(`{"text":"stale"}`), 0o600)
	got, err := s.Load()
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestConcurrentSavesRemainWhole(t *testing.T) {
	s, _ := Open(t.TempDir(), "w1")
	values := []string{strings.Repeat("a", 4096), strings.Repeat("b", 4096)}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			if err := s.Save(v); err != nil {
				t.Errorf("save: %v", err)
			}
		}(values[i%2])
	}
	wg.Wait()
	got, err := s.Load()
	if err != nil || (got != values[0] && got != values[1]) {
		t.Fatalf("torn save len=%d err=%v", len(got), err)
	}
}
