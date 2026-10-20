package badgeservice

import (
	"context"
	"errors"
	db "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/connect/dbconnect"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/badges"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/fileUsage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SaveDefinition(ctx context.Context, entity *badges.Entity) error {
	return db.ConnectContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous badges.Entity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", entity.Code).Take(&previous).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Save(entity).Error; err != nil {
			return err
		}
		return fileUsage.UpdateManagedAssetReferencesTx(tx, previous.IconURL, entity.IconURL, "badges")
	})
}
func DeleteDefinition(ctx context.Context, code string) error {
	return db.ConnectContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous badges.Entity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).Take(&previous).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if err := tx.Delete(&previous).Error; err != nil {
			return err
		}
		return fileUsage.UpdateManagedAssetReferencesTx(tx, previous.IconURL, "", "badges")
	})
}
