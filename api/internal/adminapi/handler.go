// Package adminapi implements instance-wide admin settings: the internal-
// login disable toggle (SPEC.md Decision 14) and the attachment policy
// (Decision 17: type allowlist + max size, admin-configurable, with the
// env/Helm values as defaults).
package adminapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/attachments"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

const (
	// maxAttachmentSizeCeiling bounds the admin-settable limit: uploads are
	// buffered in memory while being sniffed, so the cap protects the pods.
	maxAttachmentSizeCeiling = 100 << 20 // 100 MiB
	maxAllowlistEntries      = 100
)

var mimePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]{0,126}/[a-z0-9][a-z0-9!#$&^_.+-]{0,126}$`)

type Handler struct {
	queries *db.Queries
	cfg     config.Config
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, cfg config.Config, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, cfg: cfg, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/admin/settings", h.get)
	r.Patch("/admin/settings", h.update)
}

func (h *Handler) requireOrgAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !authctx.FromContext(r.Context()).IsOrgAdmin {
		httpx.WriteError(w, http.StatusForbidden, "org admin required")
		return false
	}
	return true
}

type settingsResponse struct {
	InternalLoginDisabled bool               `json:"internal_login_disabled"`
	UpdatedAt             pgtype.Timestamptz `json:"updated_at"`
	// The attachment policy in force (admin override, else default).
	AttachmentAllowlist        []string `json:"attachment_allowlist"`
	AttachmentMaxSizeBytes     int64    `json:"attachment_max_size_bytes"`
	AttachmentPolicyOverridden bool     `json:"attachment_policy_overridden"`
	// The deployment defaults (env/Helm), shown so the UI can offer "reset".
	AttachmentDefaultAllowlist    []string `json:"attachment_default_allowlist"`
	AttachmentDefaultMaxSizeBytes int64    `json:"attachment_default_max_size_bytes"`
}

func (h *Handler) toResponse(s db.InstanceSetting) settingsResponse {
	p := attachments.EffectivePolicy(h.cfg, s)
	return settingsResponse{
		InternalLoginDisabled:         s.InternalLoginDisabled,
		UpdatedAt:                     s.UpdatedAt,
		AttachmentAllowlist:           p.Allowlist,
		AttachmentMaxSizeBytes:        p.MaxSizeBytes,
		AttachmentPolicyOverridden:    p.Overridden,
		AttachmentDefaultAllowlist:    h.cfg.AttachmentAllowlist,
		AttachmentDefaultMaxSizeBytes: h.cfg.AttachmentMaxSizeBytes,
	}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgAdmin(w, r) {
		return
	}
	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		h.logger.Error("get instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponse(settings))
}

type updateSettingsRequest struct {
	InternalLoginDisabled *bool                    `json:"internalLoginDisabled"`
	AttachmentAllowlist   httpx.Optional[[]string] `json:"attachmentAllowlist"`
	AttachmentMaxSize     httpx.Optional[int64]    `json:"attachmentMaxSizeBytes"`
}

// update applies whichever fields are present. For the attachment policy an
// explicit null resets that field to the deployment default.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgAdmin(w, r) {
		return
	}
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.InternalLoginDisabled == nil && !req.AttachmentAllowlist.Set && !req.AttachmentMaxSize.Set {
		httpx.WriteError(w, http.StatusBadRequest, "no settings provided")
		return
	}

	params := db.UpdateInstanceSettingsParams{}
	if req.InternalLoginDisabled != nil {
		params.SetInternalLogin, params.InternalLoginDisabled = true, *req.InternalLoginDisabled
	}
	if req.AttachmentAllowlist.Set {
		params.SetAllowlist = true
		if req.AttachmentAllowlist.Value != nil {
			list, err := normalizeAllowlist(*req.AttachmentAllowlist.Value)
			if err != nil {
				httpx.WriteError(w, http.StatusBadRequest, err.Error())
				return
			}
			params.Allowlist = list
		}
	}
	if req.AttachmentMaxSize.Set {
		params.SetMaxSize = true
		if v := req.AttachmentMaxSize.Value; v != nil {
			if *v < 1 || *v > maxAttachmentSizeCeiling {
				httpx.WriteError(w, http.StatusBadRequest, "attachmentMaxSizeBytes must be between 1 and 104857600 (100 MiB)")
				return
			}
			params.MaxSizeBytes = v
		}
	}

	// One statement: a PATCH touching several settings applies all or none.
	settings, err := h.queries.UpdateInstanceSettings(r.Context(), params)
	if err != nil {
		h.logger.Error("update instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponse(settings))
}

func normalizeAllowlist(in []string) ([]string, error) {
	if len(in) == 0 || len(in) > maxAllowlistEntries {
		return nil, errBadAllowlist
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if !mimePattern.MatchString(t) {
			return nil, errBadAllowlist
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

var errBadAllowlist = &badRequest{"attachmentAllowlist must list 1 to 100 MIME types such as image/png"}

type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }
