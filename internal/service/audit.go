package service

import (
	"context"
	"encoding/json"
	"log"

	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// AuditService writes the append-only audit trail. Failures are logged,
// never surfaced: an audit write failing must not corrupt the action it
// describes, but it must be visible in the logs.
type AuditService struct {
	repo repository.AuditRepository
}

// NewAuditService constructs an AuditService.
func NewAuditService(repo repository.AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

// Actor identifies who performed an audited action.
type Actor struct {
	UserID string
	Role   string
	IP     string
}

// Record appends an audit entry.
func (a *AuditService) Record(ctx context.Context, actor Actor, action, targetType, targetID string, meta map[string]interface{}) {
	var metaJSON string
	if len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}
	if err := a.repo.Append(ctx, &models.AuditLog{
		ActorUserID: actor.UserID, ActorRole: actor.Role, Action: action,
		TargetType: targetType, TargetID: targetID, Metadata: metaJSON, IP: actor.IP,
	}); err != nil {
		log.Printf("audit: failed to record %s on %s/%s: %v", action, targetType, targetID, err)
	}
}

// List returns audit entries for the admin viewer.
func (a *AuditService) List(ctx context.Context, action, targetType, targetID string, page, pageSize int) ([]models.AuditLog, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	return a.repo.List(ctx, action, targetType, targetID, pageSize, (page-1)*pageSize)
}
