package api

import (
	"net/http"
	"path"
	"strings"
)

// PublicReviewRoute is the reviewed public personal-client route allowlist.
func PublicReviewRoute(method, route string) bool {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	if path.Clean(route) != route || strings.ContainsAny(route, "\\%") {
		return false
	}
	switch route {
	case "/api/v2/bootstrap", "/api/v2/health", "/api/v2/events", "/api/v2/metrics", "/api/v2/contexts":
		return method == http.MethodGet
	case "/api/v2/collections":
		return method == http.MethodPost
	}
	p := strings.Split(strings.TrimPrefix(route, "/api/v2/contexts/"), "/")
	if !strings.HasPrefix(route, "/api/v2/contexts/") || len(p) == 0 || p[0] == "" {
		return false
	}
	if len(p) == 1 {
		return method == http.MethodGet || method == http.MethodDelete
	}
	if len(p) == 2 {
		switch p[1] {
		case "session", "commits":
			return method == http.MethodGet
		case "comments":
			return method == http.MethodGet || method == http.MethodPost
		case "review-marks":
			return method == http.MethodGet || method == http.MethodDelete
		}
	}
	if len(p) == 3 {
		switch p[1] {
		case "comments":
			if p[2] == "export" {
				return method == http.MethodGet
			}
			if p[2] == "import" {
				return method == http.MethodPost
			}
			return method == http.MethodPatch || method == http.MethodDelete
		case "review-marks":
			return method == http.MethodPut || method == http.MethodDelete
		case "diffs":
			return p[2] == "current" && method == http.MethodGet
		case "review":
			return p[2] == "comments" && method == http.MethodGet
		}
	}
	if len(p) == 4 {
		return method == http.MethodGet && p[1] == "commits" && p[3] == "diff" || method == http.MethodPost && p[1] == "comments" && p[3] == "carry"
	}
	if len(p) == 5 {
		return p[1] == "diffs" && p[3] == "versions" && method == http.MethodGet || p[1] == "review" && p[2] == "comments" && p[4] == "resolve" && method == http.MethodPost
	}
	if len(p) == 6 && (p[1] == "diffs" || p[1] == "commits") && p[3] == "files" && (p[5] == "patch" || p[5] == "contents") {
		return method == http.MethodGet
	}
	return false
}
