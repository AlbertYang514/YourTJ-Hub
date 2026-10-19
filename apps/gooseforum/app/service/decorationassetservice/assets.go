package decorationassetservice

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	db "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/connect/dbconnect"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/imagepolicy"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/filemodel/filedata"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/decoration"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/fileUsage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var uploadedAssetName = regexp.MustCompile(`^decorations/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|webp)$`)
var ErrInvalid = errors.New("invalid decoration asset")

// ValidateArtwork checks the actual image pixels before any bytes enter storage.
func ValidateArtwork(data []byte, contentType string) error {
	if contentType != "image/png" && contentType != "image/webp" {
		return ErrInvalid
	}
	if err := imagepolicy.ValidateContent(bytes.NewReader(data), contentType); err != nil {
		return ErrInvalid
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != config.Height || config.Width < 1 || config.Width > 4096 {
		return ErrInvalid
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ErrInvalid
	}
	b := img.Bounds()
	for y := b.Min.Y + b.Dy()*35/100; y < b.Min.Y+b.Dy()*65/100; y++ {
		for x := b.Min.X + b.Dx()*35/100; x < b.Min.X+b.Dx()*65/100; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha > 257 {
				return ErrInvalid
			}
		}
	}
	return nil
}

// UploadAsset gives an unsaved draft 24 hours to use its preview URL. A saved
// definition clears the expiry; CleanupAssets reclaims abandoned/replaced files.
func UploadAsset(ctx context.Context, actorID uint64, data []byte, filename string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	contentType, ok := imagepolicy.ContentTypeForFilename(filename)
	if !ok || ValidateArtwork(data, contentType) != nil {
		return "", ErrInvalid
	}
	file, err := filedata.SaveFileFromUpload(actorID, data, strings.ToLower(path.Base(filename)), "decorations")
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		_ = filedata.DeleteByName(file.Name)
		return "", err
	}
	expires := time.Now().Add(24 * time.Hour)
	err = db.ConnectContext(ctx).Create(&fileUsage.Entity{FileName: file.Name, TargetType: fileUsage.TargetDecorationAsset, TargetId: file.Id, UsageType: fileUsage.UsageDecorationAsset, UserId: actorID, Status: fileUsage.UsageStatusActive, ExpiresAt: &expires}).Error
	if err != nil {
		_ = filedata.DeleteByName(file.Name)
		return "", err
	}
	return "/static/" + file.Name, nil
}

// ReadAsset serves only generated names with a live reference. Embedded assets
// are resolved separately by the static mount.
func ReadAsset(name string) ([]byte, time.Time, error) {
	if !uploadedAssetName.MatchString(name) {
		return nil, time.Time{}, fs.ErrNotExist
	}
	var usage fileUsage.Entity
	err := db.Connect().Where("file_name = ? AND target_type = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", name, fileUsage.TargetDecorationAsset, fileUsage.UsageStatusActive, time.Now()).First(&usage).Error
	if err != nil {
		return nil, time.Time{}, fs.ErrNotExist
	}
	file, err := filedata.GetFileByName(name)
	if err != nil {
		return nil, time.Time{}, fs.ErrNotExist
	}
	return file.Data, file.CreatedAt, nil
}

// CleanupAssets is safe to retry after an interrupted file deletion. It uses
// the existing reference row as the GC queue and never deletes a live definition.
func CleanupAssets(ctx context.Context, before time.Time, limit int) (int, error) {
	var expired []fileUsage.Entity
	if err := db.ConnectContext(ctx).Where("target_type = ? AND expires_at IS NOT NULL AND expires_at <= ?", fileUsage.TargetDecorationAsset, before).Limit(limit).Find(&expired).Error; err != nil {
		return 0, err
	}
	removed := 0
	for _, candidate := range expired {
		name := ""
		err := db.ConnectContext(ctx).Transaction(func(tx *gorm.DB) error {
			var usage fileUsage.Entity
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND expires_at IS NOT NULL AND expires_at <= ?", candidate.Id, before).First(&usage).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&decoration.Definition{}).Where("asset_url = ?", "/static/"+usage.FileName).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return tx.Model(&usage).Updates(map[string]any{"status": fileUsage.UsageStatusActive, "expires_at": nil}).Error
			}
			name = usage.FileName
			return tx.Model(&usage).Update("status", fileUsage.UsageStatusPurged).Error
		})
		if err != nil {
			return removed, err
		}
		if name == "" {
			continue
		}
		if err := filedata.DeleteByName(name); err != nil {
			return removed, err
		}
		if err := db.ConnectContext(ctx).Where("id = ? AND status = ? AND expires_at IS NOT NULL", candidate.Id, fileUsage.UsageStatusPurged).Delete(&fileUsage.Entity{}).Error; err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
