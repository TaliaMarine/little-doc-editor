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
	"github.com/jackc/pgx/v5/pgtype"
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
	// The IdP refused the request (user denied, misconfiguration, …): send the
	// browser back to the login page rather than showing raw JSON.
	if e := r.URL.Query().Get("error"); e != "" {
		h.logger.Warn("oidc provider returned an error", "error", e, "description", r.URL.Query().Get("error_description"))
		http.SetCookie(w, &http.Cookie{Name: flowCookieName, Value: "", MaxAge: -1, Path: "/", HttpOnly: true, Secure: h.secureCookies(r)})
		http.Redirect(w, r, h.loginErrorURL("provider_error"), http.StatusFound)
		return
	}
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

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "login failed: could not read claims")
		return
	}
	profile := h.profileFromClaims(idToken.Subject, raw)

	user, err := h.jitProvision(r.Context(), idToken.Issuer, idToken.Subject, profile)
	if err != nil {
		if errors.Is(err, errEmailTaken) {
			h.logger.Warn("oidc login refused: email belongs to another account", "email", profile.Email)
			http.Redirect(w, r, h.loginErrorURL("email_taken"), http.StatusFound)
			return
		}
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

// loginErrorURL is the SPA login page with the SSO-failed flag set
// (sso_error=1) and a machine-readable reason: "email_taken" (the IdP email
// belongs to a different, un-linked account — see jitProvision) or
// "provider_error".
func (h *Handler) loginErrorURL(reason string) string {
	q := url.Values{"sso_error": {"1"}, "reason": {reason}}.Encode()
	u, err := url.Parse(h.postLoginURL())
	if err != nil {
		return "/login?" + q
	}
	u.Path, u.RawQuery, u.Fragment = "/login", q, ""
	return u.String()
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

var errEmailTaken = errors.New("email already belongs to another account")

// profile is what we take from the ID token claims.
type profile struct {
	Email       string
	DisplayName string
	Username    string
	AvatarURL   string
	Groups      []string
	IsAdmin     bool
}

func (h *Handler) profileFromClaims(subject string, c map[string]any) profile {
	str := func(k string) string { s, _ := c[k].(string); return strings.TrimSpace(s) }
	p := profile{
		Email:     strings.ToLower(str("email")),
		Username:  str("preferred_username"),
		AvatarURL: str("picture"),
	}
	// An email the IdP says it has NOT verified is attacker-chosen text: never
	// store it as the account's email (it would squat that address, e.g. block
	// an invite to the real owner). Fall through to the placeholder below.
	// A missing claim is trusted: several IdPs omit email_verified.
	unverifiedEmail := false
	switch v := c["email_verified"].(type) {
	case bool:
		unverifiedEmail = !v
	case string:
		unverifiedEmail = strings.EqualFold(v, "false")
	}
	if raw, ok := c[h.cfg.OIDCGroupsClaim].([]any); ok {
		for _, g := range raw {
			if name, ok := g.(string); ok && name != "" {
				p.Groups = append(p.Groups, name)
			}
		}
	}
	if p.Groups == nil {
		p.Groups = []string{}
	}
	// Fallback chain: collab rejects an empty display name.
	for _, cand := range []string{str("name"), strings.TrimSpace(str("given_name") + " " + str("family_name")), p.Username, p.Email, subject} {
		if cand != "" {
			p.DisplayName = cand
			break
		}
	}
	if unverifiedEmail {
		p.Email = ""
	}
	if p.Email == "" {
		// users.email is NOT NULL UNIQUE; synthesize a stable placeholder.
		p.Email = subject + "@oidc.invalid"
	}
	if g := h.cfg.OIDCAdminGroup; g != "" {
		for _, have := range p.Groups {
			if have == g {
				p.IsAdmin = true
				break
			}
		}
	}
	return p
}

// jitProvision finds or creates the OIDC-linked user and refreshes its
// profile and org-admin flag from the claims on every login — the IdP is
// the source of truth, so leaving the admin group also demotes.
func (h *Handler) jitProvision(ctx context.Context, issuer, subject string, p profile) (db.User, error) {
	user, err := h.queries.GetUserByOIDCIdentity(ctx, db.GetUserByOIDCIdentityParams{
		OidcIssuer:  &issuer,
		OidcSubject: &subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Never silently link to (or take over) an existing account that
		// happens to share the email, e.g. the bootstrap admin.
		if _, lookupErr := h.queries.GetUserByEmailAnyProvider(ctx, p.Email); lookupErr == nil {
			return db.User{}, errEmailTaken
		} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return db.User{}, lookupErr
		}
		user, err = h.queries.CreateOIDCUser(ctx, db.CreateOIDCUserParams{
			Email:       p.Email,
			DisplayName: p.DisplayName,
			OidcIssuer:  &issuer,
			OidcSubject: &subject,
		})
	}
	if err != nil {
		return db.User{}, err
	}
	if err := h.syncGroups(ctx, user.ID, p.Groups); err != nil {
		return db.User{}, err
	}
	return h.queries.UpdateOIDCUserProfile(ctx, db.UpdateOIDCUserProfileParams{
		ID:          user.ID,
		DisplayName: p.DisplayName,
		Username:    nilIfEmpty(p.Username),
		AvatarUrl:   nilIfEmpty(p.AvatarURL),
		IdpGroups:   p.Groups,
		IsOrgAdmin:  p.IsAdmin,
	})
}

const (
	maxGroups        = 200
	maxGroupNameSize = 200
)

// syncGroups mirrors the IdP's group claim into groups/group_members on every
// login: unknown groups are created, memberships the IdP no longer reports are
// dropped. Space roles granted to a group (space_members.group_id) then apply
// to its members (api GetSpaceMemberRole; collab access.ts must do the same).
func (h *Handler) syncGroups(ctx context.Context, userID pgtype.UUID, groups []string) error {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g = strings.TrimSpace(g); g != "" && len(g) <= maxGroupNameSize && len(names) < maxGroups {
			names = append(names, g)
		}
	}
	return h.queries.SyncUserGroups(ctx, db.SyncUserGroupsParams{Names: names, UserID: userID})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
