package migration

import (
	"testing"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/connect/dbconnect"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/badges"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/pageConfig"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/posts"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/userBadges"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/users"
	xpmodel "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/xp"
)

// A deployment already at v30 must still run the legacy pending adoption.
func TestVersionedMigrationRunsPublicationAdoptionFrom30(t *testing.T) {
	conn := dbconnect.Connect()
	models := []any{&pageConfig.Entity{}, &posts.Entity{}, &users.EntityComplete{}, &badges.Entity{}, &userBadges.Entity{}}
	models = append(models, xpmodel.Models()...)
	if err := conn.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	if err := pageConfig.SyncMigrationVersion(30); err != nil {
		t.Fatal(err)
	}
	if err := runVersionedDataMigrations(); err != nil {
		t.Fatal(err)
	}
	if got := pageConfig.GetMigrationVersion(); got != 33 {
		t.Fatalf("migration stopped at %d; expected initial XP rules at version 33", got)
	}
	var dailyRule xpmodel.RewardRule
	if err := conn.Where("rule_set_version = ? AND rule_key = ?", 1, "daily_base").Take(&dailyRule).Error; err != nil {
		t.Fatalf("initial daily XP rule missing after migration: %v", err)
	}
	if dailyRule.RewardXP != 5 || dailyRule.DailyCountLimit == nil || *dailyRule.DailyCountLimit != 1 {
		t.Fatalf("initial daily XP rule = %+v; want confirmed +5 XP once per day", dailyRule)
	}
}
