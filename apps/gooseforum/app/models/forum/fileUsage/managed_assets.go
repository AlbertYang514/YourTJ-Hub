package fileUsage

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/badges"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/decoration"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var uploadedAssetName = regexp.MustCompile(`^(decorations|badges|badges/normalized)/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|webp|gif)$`)
var ErrInvalidManagedAsset = errors.New("invalid managed artwork reference")

func IsManagedAssetName(name string) bool { return uploadedAssetName.MatchString(name) }
func assetTarget(prefix string) string {
	if prefix == "badges" {
		return TargetBadgeAsset
	}
	return TargetDecorationAsset
}

func uploadedName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/static/") {
		return ""
	}
	name := strings.TrimPrefix(u.Path, "/static/")
	if !uploadedAssetName.MatchString(name) {
		return ""
	}
	return name
}

func ManagedAssetReferenceCount(tx *gorm.DB, name string, count *int64) error {
	if strings.HasPrefix(name, "badges/") {
		return tx.Model(&badges.Entity{}).Where("icon_url = ? OR icon_url LIKE ? OR icon_url LIKE ?", "/static/"+name, "/static/"+name+"?%", "/static/"+name+"#%").Count(count).Error
	}
	return tx.Model(&decoration.Definition{}).Where("asset_url = ?", "/static/"+name).Count(count).Error
}

// UpdateManagedAssetReferencesTx binds the new upload and releases replaced artwork in the definition transaction.
func UpdateManagedAssetReferencesTx(tx *gorm.DB, oldURL, newURL, prefix string) error {
	oldName, newName := uploadedName(oldURL), uploadedName(newURL)
	if oldName == newName {
		return nil
	}
	if newName != "" {
		if !strings.HasPrefix(newName, prefix+"/") {
			return ErrInvalidManagedAsset
		}
		var usage Entity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("file_name = ? AND target_type = ? AND status = ?", newName, assetTarget(prefix), UsageStatusActive).First(&usage).Error; err != nil {
			return ErrInvalidManagedAsset
		}
		if usage.ExpiresAt != nil && time.Now().After(*usage.ExpiresAt) {
			return ErrInvalidManagedAsset
		}
		if err := tx.Model(&usage).Update("expires_at", nil).Error; err != nil {
			return err
		}
	}
	if oldName != "" {
		var count int64
		if err := ManagedAssetReferenceCount(tx, oldName, &count); err != nil {
			return err
		}
		if count == 0 {
			return tx.Model(&Entity{}).Where("file_name = ? AND target_type = ?", oldName, assetTarget(prefix)).Updates(map[string]any{"status": UsageStatusPurged, "expires_at": time.Now()}).Error
		}
	}
	return nil
}
