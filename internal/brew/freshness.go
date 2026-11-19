package brew

import (
	"os"
	"path/filepath"
	"time"
)

// ListsUpdatedAt is when `brew update` last refreshed the package lists,
// wherever it ran (Brewctl or a terminal); zero if it can't tell.
func (c *Client) ListsUpdatedAt() time.Time {
	return listsUpdatedAt(c.Repository, c.Cache)
}

// listsUpdatedAt reads the signal from files `brew update` writes, without
// running brew (cmd/update.sh and api.rb, Homebrew 4 to 7):
//
//   - By default Homebrew installs from its JSON API. update.sh's
//     fetch_api_file downloads api/internal/packages.<tag>.jws.json (older
//     releases: api/formula.jws.json and api/cask.jws.json) into the cache
//     and touches it after every successful check, including a 304 "not
//     modified". A failed download leaves it alone, so its mtime is when the
//     lists were last confirmed current.
//   - With the API off (HOMEBREW_NO_INSTALL_FROM_API) the lists are the taps'
//     git checkouts, and update.sh touches each repository's .git/FETCH_HEAD
//     when it checks it.
//
// FETCH_HEAD is touched before the fetch, even when it then fails, so it only
// counts when there is no API cache at all.
func listsUpdatedAt(repo, cache string) time.Time {
	if cache != "" {
		api := filepath.Join(cache, "api")
		files, _ := filepath.Glob(filepath.Join(api, "internal", "packages.*.jws.json"))
		files = append(files, filepath.Join(api, "formula.jws.json"), filepath.Join(api, "cask.jws.json"))
		if t := newestMtime(files); !t.IsZero() {
			return t
		}
	}
	if repo == "" {
		return time.Time{}
	}
	heads, _ := filepath.Glob(filepath.Join(repo, "Library", "Taps", "*", "*", ".git", "FETCH_HEAD"))
	return newestMtime(append(heads, filepath.Join(repo, ".git", "FETCH_HEAD")))
}

func newestMtime(paths []string) time.Time {
	var newest time.Time
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	return newest
}
