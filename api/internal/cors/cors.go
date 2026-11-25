// Package cors is the api's opt-in cross-origin support, for deployments
// where the SPA is served from a different origin than the api (the Helm
// chart's separate api/collab hosts). Off unless CORS_ALLOWED_ORIGINS lists
// at least one origin.
//
// Deliberately minimal and strict:
//   - exact-match allow-list of origins; there is no wildcard, ever (so no
//     "*" alongside an Authorization header);
//   - no credentials: the api authenticates with a bearer token in the
//     Authorization header, never cookies, so Access-Control-Allow-Credentials
//     is never sent and ambient browser credentials never apply;
//   - an origin outside the list gets no CORS headers at all (the browser
//     blocks the response) and a preflight from it is refused with 403.
package cors

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	allowMethods  = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	allowHeaders  = "Authorization, Content-Type"
	exposeHeaders = "Content-Disposition, Retry-After"
	preflightMax  = "600"
)

// NormalizeOrigin validates one CORS_ALLOWED_ORIGINS entry: an http(s) origin
// (scheme://host[:port]) with no path, query or userinfo. It returns the
// lowercased form browsers send in the Origin header.
func NormalizeOrigin(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "*" {
		return "", fmt.Errorf("CORS_ALLOWED_ORIGINS must list explicit origins, not %q", s)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("CORS_ALLOWED_ORIGINS entry %q is not an origin like https://docs.example.com", raw)
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

// Middleware returns the CORS middleware for the given origins (already
// validated by config), or a pass-through when the list is empty.
func Middleware(origins []string) func(http.Handler) http.Handler {
	if len(origins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.ToLower(o)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			// The response depends on Origin, so caches must key on it.
			w.Header().Add("Vary", "Origin")
			_, ok := allowed[strings.ToLower(origin)]
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if preflight {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				if !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", allowMethods)
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				h.Set("Access-Control-Max-Age", preflightMax)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}
