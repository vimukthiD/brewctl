package brew

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touchAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestListsUpdatedAt(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }

	t.Run("nothing on disk", func(t *testing.T) {
		if got := listsUpdatedAt(t.TempDir(), t.TempDir()); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
		if got := listsUpdatedAt("", ""); !got.IsZero() {
			t.Errorf("empty paths: got %v, want zero", got)
		}
	})

	t.Run("API cache wins over FETCH_HEAD", func(t *testing.T) {
		repo, cache := t.TempDir(), t.TempDir()
		// FETCH_HEAD is touched even when the fetch then fails, so a newer
		// one must not make stale lists look fresh.
		touchAt(t, filepath.Join(repo, ".git", "FETCH_HEAD"), at(5))
		touchAt(t, filepath.Join(cache, "api", "internal", "packages.arm64_tahoe.jws.json"), at(2))
		if got := listsUpdatedAt(repo, cache); !got.Equal(at(2)) {
			t.Errorf("got %v, want %v", got, at(2))
		}
	})

	t.Run("newest API file, sidecars ignored", func(t *testing.T) {
		cache := t.TempDir()
		api := filepath.Join(cache, "api")
		touchAt(t, filepath.Join(api, "formula.jws.json"), at(1))
		touchAt(t, filepath.Join(api, "cask.jws.json"), at(3))
		touchAt(t, filepath.Join(api, "internal", "packages.arm64_sequoia.jws.json"), at(2))
		// Sidecars and derived name lists, rewritten at other times.
		touchAt(t, filepath.Join(api, "internal", "packages.arm64_sequoia.jws.json.payload"), at(9))
		touchAt(t, filepath.Join(api, "internal", "packages.arm64_sequoia.jws.json.etag"), at(9))
		touchAt(t, filepath.Join(api, "formula_names.txt"), at(9))
		if got := listsUpdatedAt("", cache); !got.Equal(at(3)) {
			t.Errorf("got %v, want %v", got, at(3))
		}
	})

	t.Run("git taps when the API is off", func(t *testing.T) {
		repo := t.TempDir()
		touchAt(t, filepath.Join(repo, ".git", "FETCH_HEAD"), at(1))
		touchAt(t, filepath.Join(repo, "Library", "Taps", "homebrew", "homebrew-core", ".git", "FETCH_HEAD"), at(4))
		touchAt(t, filepath.Join(repo, "Library", "Taps", "someone", "homebrew-tools", ".git", "FETCH_HEAD"), at(2))
		if got := listsUpdatedAt(repo, t.TempDir()); !got.Equal(at(4)) {
			t.Errorf("got %v, want %v", got, at(4))
		}
	})

	t.Run("a directory named like a list doesn't count", func(t *testing.T) {
		cache := t.TempDir()
		if err := os.MkdirAll(filepath.Join(cache, "api", "formula.jws.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := listsUpdatedAt("", cache); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
}
