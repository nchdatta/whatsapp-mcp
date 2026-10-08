package wa

import "testing"

func TestIsAppStateNoise(t *testing.T) {
	for msg, want := range map[string]bool{
		"Failed to do initial fetch of app state regular_low: failed to decode app state regular_low patches: failed to verify snapshot: failed to verify patch v173: mismatching LTHash": true,
		"Failed to sync app state after notification: failed to decode app state regular_low patches: failed to verify patch v55: mismatching LTHash":                                     true,
		"Failed to decrypt message": false,
	} {
		if got := isAppStateNoise(msg); got != want {
			t.Errorf("isAppStateNoise(%q) = %v, want %v", msg, got, want)
		}
	}
}
