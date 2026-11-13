package api

import "testing"

// Personal clients (desktop, pinned TLS, gateways) reach comment threads
// through the public allowlist, and only with the methods the API defines.
func TestPublicReviewRouteAllowsCommentThreads(t *testing.T) {
	for _, allowed := range []struct{ method, route string }{
		{"POST", "/api/v2/contexts/c/comments/m/replies"},
		{"PATCH", "/api/v2/contexts/c/comments/m/replies/r"},
		{"DELETE", "/api/v2/contexts/c/comments/m/replies/r"},
		{"POST", "/api/v2/contexts/c/review/comments/m/resolve"},
	} {
		if !PublicReviewRoute(allowed.method, allowed.route) {
			t.Errorf("refused %s %s", allowed.method, allowed.route)
		}
	}
	for _, refused := range []struct{ method, route string }{
		{"GET", "/api/v2/contexts/c/comments/m/replies"},
		{"POST", "/api/v2/contexts/c/comments/m/replies/r"},
		{"PUT", "/api/v2/contexts/c/comments/m/replies/r"},
		{"PATCH", "/api/v2/contexts/c/comments/m/other/r"},
		{"DELETE", "/api/v2/contexts/c/comments/m/replies/../r"},
	} {
		if PublicReviewRoute(refused.method, refused.route) {
			t.Errorf("accepted %s %s", refused.method, refused.route)
		}
	}
}
