package decorationservice

import (
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/fileUsage"
	"gorm.io/gorm"
)

func updateAssetReferencesTx(tx *gorm.DB, oldURL, newURL string) error {
	return fileUsage.UpdateManagedAssetReferencesTx(tx, oldURL, newURL, "decorations")
}
