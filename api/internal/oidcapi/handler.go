// Package oidcapi implements the OIDC authorization-code + PKCE login flow
// against the single instance-wide issuer (SPEC.md Decision 14), with
// JIT provisioning of OIDC-linked users on first login.
package oidcapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessionrecord"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
)

const flowCookieName = "ldeoidcflow"

type Handler struct {
	queries  *db.Queries
	cfg      config.Config
	logger   *slog.Logger
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

// NewHandler constructs the handler and, if OIDC is enabled, performs issuer
// discovery immediately — the same "fail fast on bad config at startup"
// philosophy as the rest of this service's config loading. A misconfigured
// or unreachable issuer at boot is a deployment problem to surface loudly,
// not to paper over with lazy retries.
func NewHandler(ctx context.Context, queries *db.Queries, cfg config.Config, logger *slog.Logger) (*Handler, error) {
	h := &Handler{queries: queries, cfg: cfg, logger: logger}
	if !cfg.OIDCEnabled {
		return h, nil
	}

	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	h.provider = provider
	h.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})
	endpoint := provider.Endpoint()
	if cfg.OIDCClientSecret == "" {
		// Public client: PKCE only, client_id in the body, no secret.
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	h.oauth = oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     endpoint,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return h, nil
}

func (h *Handler) PublicRoutes(r chi.Router) {
	// Lets the login page decide whether to show the SSO button.
	r.Get("/auth/oidc/status", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"enabled": h.cfg.OIDCEnabled})
	})
	if !h.cfg.OIDCEnabled {
		return
	}
	r.Get("/auth/oidc/login", h.login)
	r.Get("/auth/oidc/callback", h.callback)
}

type flowClaims struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	jwt.RegisteredClaims
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomString(32)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to start login")
		return
	}
	verifier := oauth2.GenerateVerifier()
	nonce, err := randomString(32)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to start login")
		return
	}

	flowToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, flowClaims{
		State:    state,
		Verifier: verifier,
		Nonce:    nonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}).SignedString([]byte(h.cfg.SessionJWTSecret))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to start login")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    flowToken,
		HttpOnly: true,
		Secure:   h.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
		Path:     "/",
	})

	authURL := h.oauth.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(flowCookieName)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing login flow cookie — the login attempt may have expired")
		return
	}
	var flow flowClaims
	if _, err := jwt.ParseWithClaims(cookie.Value, &flow, func(t *jwt.Token) (any, error) {
		return []byte(h.cfg.SessionJWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired()); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid or expired login flow")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.secureCookies(r)})

	// The flow cookie is signed with the same secret as session tokens, so
	// a session token planted as this cookie would also verify — it just
	// has no state/verifier. Requiring a non-empty state rejects that.
	if flow.State == "" || flow.Verifier == "" || r.URL.Query().Get("state") != flow.State {
		httpx.WriteError(w, http.StatusBadRequest, "state mismatch")
		return
	}

	oauth2Token, err := h.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		h.logger.Error("oidc code exchange failed", "error", err)
		httpx.WriteError(w, http.StatusUnauthorized, "login failed")
		return
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "login failed: no id_token in response")
		return
	}
	idToken, err := h.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		h.logger.Error("oidc id_token verification failed", "error", err)
		httpx.WriteError(w, http.StatusUnauthorized, "login failed")
		return
	}
	if idToken.Nonce != flow.Nonce {
		httpx.WriteError(w, http.StatusUnauthorized, "nonce mismatch")
		return
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "login failed: could not read claims")
		return
	}
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Email
	}
	if displayName == "" {
		displayName = idToken.Subject // collab rejects an empty display name
	}

	user, err := h.jitProvision(r.Context(), idToken.Issuer, idToken.Subject, claims.Email, displayName)
	if err != nil {
		h.logger.Error("oidc JIT provisioning failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if user.Disabled {
		httpx.WriteError(w, http.StatusForbidden, "this account has been disabled")
		return
	}

	userID := db.FromPgUUID(user.ID)
	token, err := sessiontoken.IssueForUser(h.cfg.SessionJWTSecret, userID, user.DisplayName, h.cfg.SessionTTL)
	if err != nil {
		h.logger.Error("issuing session token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if _, err := h.queries.CreateUserSession(r.Context(), db.CreateUserSessionParams{
		UserID:    user.ID,
		TokenHash: sessionrecord.HashToken(token),
		ExpiresAt: sessionrecord.ExpiresAt(h.cfg.SessionTTL),
	}); err != nil {
		h.logger.Warn("recording session row failed", "error", err)
	}

	// Hand the session to the SPA in the URL fragment: fragments are never
	// sent to servers or written to access logs, unlike a query string.
	frag := url.Values{"token": {token}, "displayName": {user.DisplayName}}
	http.Redirect(w, r, h.postLoginURL()+"#"+frag.Encode(), http.StatusFound)
}

func (h *Handler) postLoginURL() string {
	if h.cfg.OIDCPostLoginURL != "" {
		return h.cfg.OIDCPostLoginURL
	}
	if u, err := url.Parse(h.cfg.OIDCRedirectURL); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/auth/callback"
	}
	return "/auth/callback"
}

func (h *Handler) jitProvision(ctx context.Context, issuer, subject, email, displayName string) (db.User, error) {
	user, err := h.queries.GetUserByOIDCIdentity(ctx, db.GetUserByOIDCIdentityParams{
		OidcIssuer:  &issuer,
		OidcSubject: &subject,
	})
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, err
	}

	return h.queries.CreateOIDCUser(ctx, db.CreateOIDCUserParams{
		Email:       email,
		DisplayName: displayName,
		OidcIssuer:  &issuer,
		OidcSubject: &subject,
	})
}

// secureCookies reports whether to set the Secure cookie attribute. r.TLS
// alone is nil behind a TLS-terminating ingress (the normal production
// setup, SPEC.md §7.4), so the registered https redirect URL also counts.
func (h *Handler) secureCookies(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(h.cfg.OIDCRedirectURL, "https://")
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
