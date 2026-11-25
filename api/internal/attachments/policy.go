package attachments

import (
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

// Policy is the attachment upload policy in force (Decision 17).
type Policy struct {
	Allowlist    []string
	MaxSizeBytes int64
	// Overridden is true when an org admin has set either value in
	// instance_settings (otherwise both are the env/Helm defaults).
	Overridden bool
}

// EffectivePolicy resolves the policy: each instance_settings value, when
// non-NULL, overrides the deployment default from cfg.
func EffectivePolicy(cfg config.Config, s db.InstanceSetting) Policy {
	p := Policy{Allowlist: cfg.AttachmentAllowlist, MaxSizeBytes: cfg.AttachmentMaxSizeBytes}
	if s.AttachmentAllowlist != nil {
		p.Allowlist, p.Overridden = s.AttachmentAllowlist, true
	}
	if s.AttachmentMaxSizeBytes != nil {
		p.MaxSizeBytes, p.Overridden = *s.AttachmentMaxSizeBytes, true
	}
	return p
}
