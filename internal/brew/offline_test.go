package brew

import "testing"

func TestNetworkIssue(t *testing.T) {
	offline := []string{
		"fatal: unable to access 'https://github.com/Homebrew/brew/': Could not resolve host: github.com",
		"fatal: unable to access 'https://github.com/Homebrew/brew/': Failed to connect to github.com port 443 after 75003 ms: Couldn't connect to server",
		"fatal: unable to access 'https://github.com/Homebrew/brew/': Operation timed out after 300000 milliseconds with 0 out of 0 bytes received",
		"curl: (6) Could not resolve host: formulae.brew.sh",
		"curl: (7) Failed to connect to ghcr.io port 443",
		"curl: (28) Connection timed out after 15001 milliseconds",
		"ssh: connect to host github.com port 22: Network is unreachable",
		"fatal: unable to access 'https://github.com/x/y/': Could not resolve proxy: proxy.example",
	}
	for _, l := range offline {
		if !networkIssue(l) {
			t.Errorf("not recognised as a network failure: %q", l)
		}
	}
	other := []string{
		"Error: Fetching /opt/homebrew failed!",
		"Error: Failed to download https://formulae.brew.sh/api/internal/packages.arm64_tahoe.jws.json!",
		"curl: (22) The requested URL returned error: 404",
		"fatal: couldn't find remote ref refs/heads/master",
		"Already up-to-date.",
		"==> Downloading https://ghcr.io/v2/homebrew/core/wget/manifests/1.25.0",
	}
	for _, l := range other {
		if networkIssue(l) {
			t.Errorf("wrongly taken for a network failure: %q", l)
		}
	}
}

func TestRunnerFlagsOffline(t *testing.T) {
	r := &Runner{emit: func(string, any) {}, queue: make(chan *Job, 4), nextID: 1}
	j := &Job{ID: 1, Output: []string{}}
	r.jobs = append(r.jobs, j)
	r.appendLines(j, []string{"==> Fetching", "fatal: unable to access 'https://github.com/Homebrew/brew/': Could not resolve host: github.com"})
	if !j.Offline {
		t.Error("job not flagged as offline")
	}
}
