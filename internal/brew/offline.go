package brew

import (
	"regexp"
	"strings"
)

// Wording from curl and git (which `brew update` runs) when the network is
// down or a server can't be reached. update.sh runs curl with --silent, so
// offline it's git's "fatal: unable to access …: Could not resolve host"
// that shows, followed by brew's own "Fetching … failed!".
var networkPhrases = []string{
	"could not resolve host",
	"couldn't resolve host",
	"could not resolve proxy",
	"temporary failure in name resolution",
	"nodename nor servname provided",
	"failed to connect to",
	"couldn't connect to server",
	"could not connect to server",
	"operation timed out",
	"connection timed out",
	"network is unreachable",
	"no route to host",
	"the internet connection appears to be offline",
}

// curl's exit codes for a host it couldn't resolve (6), reach (7) or that
// didn't answer in time (28), as curl prints them: "curl: (6) …".
var curlNetworkExit = regexp.MustCompile(`curl: \((6|7|28)\)`)

// networkIssue reports whether a line of output says the network failed.
func networkIssue(line string) bool {
	if curlNetworkExit.MatchString(line) {
		return true
	}
	l := strings.ToLower(line)
	for _, p := range networkPhrases {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}
