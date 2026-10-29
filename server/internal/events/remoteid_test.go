package events

import (
	"regexp"
	"testing"
)

// Google accepts client-chosen event ids of 5 to 1024 characters from a-v and 0-9 only; anything else is a 400
// "Invalid resource id value". The fake Google does not check, so pin the alphabet here.
func TestRemoteIDIsGoogleSafe(t *testing.T) {
	ok := regexp.MustCompile(`^[a-v0-9]{5,}$`)
	for _, seed := range []string{"a", "01M64Y4WA37YCYSRQ3SRR2X7EW", "idem-key/with spaces", "ZZZ"} {
		if id := remoteID(seed); !ok.MatchString(id) {
			t.Fatalf("remoteID(%q) = %q, not a valid Google event id", seed, id)
		}
	}
	if remoteID("") != "" {
		t.Fatal("empty seed must give an empty id")
	}
	if remoteID("x") != remoteID("x") {
		t.Fatal("same seed must give the same id")
	}
}
