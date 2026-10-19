package decorationservice

import (
	"regexp"
	"strings"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/decoration"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/fileUsage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var uploadedAssetURL = regexp.MustCompile(`^/static/decorations/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|webp)$`)

func updateAssetReferencesTx(tx *gorm.DB, oldURL, newURL string) error {
	if oldURL == newURL {
		return nil
	}
	if uploadedAssetURL.MatchString(newURL) {
		name := strings.TrimPrefix(newURL, "/static/")
		var usage fileUsage.Entity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("file_name = ? AND target_type = ? AND status = ?", name, fileUsage.TargetDecorationAsset, fileUsage.UsageStatusActive).First(&usage).Error; err != nil {
			return ErrInvalid
		}
		if usage.ExpiresAt != nil && time.Now().After(*usage.ExpiresAt) {
			return ErrInvalid
		}
		if err := tx.Model(&usage).Update("expires_at", nil).Error; err != nil {
			return err
		}
	}
	if uploadedAssetURL.MatchString(oldURL) {
		var count int64
		if err := tx.Model(&decoration.Definition{}).Where("asset_url = ?", oldURL).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			expires := time.Now()
			return tx.Model(&fileUsage.Entity{}).Where("file_name = ? AND target_type = ?", strings.TrimPrefix(oldURL, "/static/"), fileUsage.TargetDecorationAsset).Updates(map[string]any{"status": fileUsage.UsageStatusPurged, "expires_at": expires}).Error
		}
	}
	return nil
}
