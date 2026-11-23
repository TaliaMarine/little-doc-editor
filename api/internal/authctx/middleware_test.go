package authctx

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_RejectsWithoutBypass(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	mw := Middleware(false, "test-secret", nil, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if handlerCalled {
		t.Error("expected next handler not to be called when auth is rejected")
	}
}

func TestMiddleware_RejectsGarbageBearerToken(t *testing.T) {
	// Neither this nor the "missing token" case above ever reaches the DB
	// lookup, so a nil *db.Queries is safe here — the real-token/DB-lookup
	// path is covered by internal/integrationtest instead.
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	mw := Middleware(false, "test-secret", nil, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if handlerCalled {
		t.Error("expected next handler not to be called when auth is rejected")
	}
}

func TestMiddleware_BypassGrantsFixedAdminIdentity(t *testing.T) {
	var gotIdentity Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdentity = FromContext(r.Context())
	})

	mw := Middleware(true, "test-secret", nil, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (next handler's default), got %d", rec.Code)
	}
	if !gotIdentity.IsOrgAdmin {
		t.Error("expected bypass identity to be an org admin")
	}
	if gotIdentity.UserID == nil {
		t.Fatal("expected bypass identity to have a UserID")
	}
	if *gotIdentity.UserID != localDevAdminUserID {
		t.Errorf("expected fixed dev admin UUID, got %s", *gotIdentity.UserID)
	}
	if gotIdentity.IsGuest() {
		t.Error("expected bypass identity not to be a guest")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
