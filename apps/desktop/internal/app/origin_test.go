package app

import "testing"

func TestBridgeTrustsExactDesktopOrigin(t *testing.T) {
	const origin = "http://127.0.0.1:3210"
	for _, raw := range []string{origin, origin + "/streams/review?scope=all#comment"} {
		if !sameOrigin(raw, origin) {
			t.Errorf("refused desktop route %q", raw)
		}
	}
	for _, raw := range []string{"http://localhost:3210", "http://127.0.0.1:3211", "https://127.0.0.1:3210", "http://127.0.0.1:3210@attacker.example", "http://user@127.0.0.1:3210", "about:blank", "file:///tmp/review.html", "javascript:alert(1)", "http://127.0.0.1:3210.attacker.example"} {
		if sameOrigin(raw, origin) {
			t.Errorf("trusted foreign page %q", raw)
		}
	}
}
