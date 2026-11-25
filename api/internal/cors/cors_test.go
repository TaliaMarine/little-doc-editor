package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func run(t *testing.T, origins []string, method string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	h := Middleware(origins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	req := httptest.NewRequest(method, "/api/v1/pages", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDisabledWhenNoOrigins(t *testing.T) {
	rec := run(t, nil, "GET", map[string]string{"Origin": "https://a.example.com"})
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("must be a no-op: %d %v", rec.Code, rec.Header())
	}
}

func TestPreflight(t *testing.T) {
	origins := []string{"https://docs.example.com"}
	pre := map[string]string{"Origin": "https://docs.example.com", "Access-Control-Request-Method": "PATCH"}
	rec := run(t, origins, "OPTIONS", pre)
	h := rec.Header()
	if rec.Code != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != "https://docs.example.com" {
		t.Fatalf("allowed preflight: %d %v", rec.Code, h)
	}
	if h.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" || h.Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("headers/credentials wrong: %v", h)
	}
	for _, m := range []string{"PATCH", "PUT", "DELETE"} {
		if !contains(h.Get("Access-Control-Allow-Methods"), m) {
			t.Errorf("missing method %s", m)
		}
	}

	pre["Origin"] = "https://evil.example.com"
	rec = run(t, origins, "OPTIONS", pre)
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed preflight must be refused: %d %v", rec.Code, rec.Header())
	}
}

func TestActualRequests(t *testing.T) {
	origins := []string{"https://docs.example.com"}
	rec := run(t, origins, "GET", map[string]string{"Origin": "https://docs.example.com"})
	h := rec.Header()
	if rec.Code != http.StatusTeapot || h.Get("Access-Control-Allow-Origin") != "https://docs.example.com" ||
		h.Get("Access-Control-Expose-Headers") != "Content-Disposition, Retry-After" || h.Get("Vary") != "Origin" {
		t.Fatalf("allowed origin: %d %v", rec.Code, h)
	}
	if h.Get("Access-Control-Allow-Credentials") != "" {
		t.Error("credentials must never be allowed")
	}

	rec = run(t, origins, "GET", map[string]string{"Origin": "https://evil.example.com"})
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin must get no CORS headers: %v", rec.Header())
	}
	rec = run(t, origins, "GET", map[string]string{"Origin": "null"})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("null origin must not be allowed")
	}
	rec = run(t, origins, "GET", nil) // same-origin / non-browser
	if rec.Code != http.StatusTeapot || rec.Header().Get("Vary") != "" {
		t.Fatalf("no Origin: untouched: %v", rec.Header())
	}
	// A plain OPTIONS without Access-Control-Request-Method is not a preflight.
	rec = run(t, origins, "OPTIONS", map[string]string{"Origin": "https://docs.example.com"})
	if rec.Code != http.StatusTeapot {
		t.Errorf("non-preflight OPTIONS should reach the handler, got %d", rec.Code)
	}
}

func TestNormalizeOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://Docs.example.com": "https://docs.example.com",
		" http://localhost:5173/ ": "http://localhost:5173",
	} {
		if got, err := NormalizeOrigin(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"*", "docs.example.com", "https://x.com/path", "ftp://x.com", "https://u@x.com", "https://x.com?a=1", ""} {
		if _, err := NormalizeOrigin(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func contains(s, sub string) bool {
	for _, p := range splitComma(s) {
		if p == sub {
			return true
		}
	}
	return false
}

func splitComma(s string) (out []string) {
	cur := ""
	for _, c := range s {
		if c == ',' {
			out = append(out, cur)
			cur = ""
		} else if c != ' ' {
			cur += string(c)
		}
	}
	return append(out, cur)
}
