package xpservice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/pageConfig"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/pointsRecord"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/userPoints"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/users"
	xpmodel "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/xp"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyRewardIsIdempotentAndDailyBaseDoesNotRequireActivity(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 7)
	if err := db.Create(&userPoints.Entity{UserId: 7, CurrentPoints: 12}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pointsRecord.Entity{UserId: 7, Action: "legacy", PointsChange: 4}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	now := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)

	first, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 7, RuleKey: DailyBaseRuleKey, SourceKey: "daily:7:2026-01-02", OccurredAt: now})
	if err != nil {
		t.Fatalf("daily base award: %v", err)
	}
	if first.Entry.Delta != 5 || first.AlreadyApplied {
		t.Fatalf("first result = %+v, want new +5 entry", first)
	}
	replay, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 7, RuleKey: DailyBaseRuleKey, SourceKey: "daily:7:2026-01-02", OccurredAt: now.Add(time.Minute)})
	if err != nil || !replay.AlreadyApplied {
		t.Fatalf("replay = %+v, %v; want idempotent result", replay, err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 7, RuleKey: DailyBaseRuleKey, SourceKey: "daily:7:second", OccurredAt: now.Add(time.Hour)}); !errors.Is(err, ErrRewardLimit) {
		t.Fatalf("second daily base award error = %v, want daily count limit", err)
	}
	if got, err := svc.CurrentBalance(context.Background(), 7); err != nil || got != 5 {
		t.Fatalf("current balance = %d, %v; want 5", got, err)
	}
	var entries int64
	if err := db.Model(&xpmodel.Entry{}).Count(&entries).Error; err != nil || entries != 1 {
		t.Fatalf("ledger entries = %d, %v; want exactly one", entries, err)
	}
	var oldPoints userPoints.Entity
	if err := db.Where("user_id = ?", 7).Take(&oldPoints).Error; err != nil || oldPoints.CurrentPoints != 12 {
		t.Fatalf("legacy points balance = %+v, %v; XP must not change it", oldPoints, err)
	}
	var oldRecordCount int64
	if err := db.Model(&pointsRecord.Entity{}).Where("user_id = ?", 7).Count(&oldRecordCount).Error; err != nil || oldRecordCount != 1 {
		t.Fatalf("legacy point record count = %d, %v; want unchanged", oldRecordCount, err)
	}
	var account users.EntityComplete
	if err := db.Where("id = ?", 7).Take(&account).Error; err != nil || account.Prestige != 19 {
		t.Fatalf("legacy prestige = %d, %v; XP must not change it", account.Prestige, err)
	}
	var usage xpmodel.DailyUsage
	if err := db.Where("user_id = ?", 7).Take(&usage).Error; err != nil || usage.PositiveXP != 5 || usage.UsageDate != "2026-01-02" {
		t.Fatalf("daily usage = %+v, %v; want 5 XP for Shanghai date 2026-01-02", usage, err)
	}
}

func TestRewardRequiresCurrentEligibleHumanAccount(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 1)
	if err := db.Create(&users.EntityComplete{Id: 2, Username: "bot", ActorType: users.ActorTypeBot, IsActivated: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&users.EntityComplete{Id: 3, Username: "frozen", IsFrozen: 1, IsActivated: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&users.EntityComplete{Id: 4, Username: "unverified"}).Error; err != nil {
		t.Fatal(err)
	}
	verifiedOnly := pageConfig.SecurityAndRegistration{EnableEmailVerification: true}
	svc := NewWithPolicy(db, verifiedOnly)
	for _, id := range []uint64{2, 3, 4} {
		_, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: id, RuleKey: DailyBaseRuleKey, SourceKey: fmt.Sprintf("award:%d", id), OccurredAt: time.Now()})
		if !errors.Is(err, ErrIneligible) {
			t.Errorf("award to ineligible account %d error = %v", id, err)
		}
	}
	withoutVerification := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	if _, err := withoutVerification.ApplyReward(context.Background(), AwardRequest{UserID: 1, RuleKey: DailyBaseRuleKey, SourceKey: "award:1", OccurredAt: time.Now()}); err != nil {
		t.Fatalf("human account should be eligible when verification is disabled at account level: %v", err)
	}
}

func TestConcurrentRewardsDoNotLoseBalanceOrDuplicateSource(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 9)
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{
		Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: time.Now().UTC(),
		Rules: []xpmodel.RewardRule{{RuleKey: "concurrent", Enabled: true, RewardXP: 1}},
	}); err != nil {
		t.Fatalf("publish concurrent rule: %v", err)
	}

	const workers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := svc.ApplyReward(context.Background(), AwardRequest{
				UserID: 9, RuleKey: "concurrent", SourceKey: fmt.Sprintf("concurrent:%d", index), OccurredAt: time.Now().UTC(),
			})
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent reward failed: %v", err)
		}
	}
	if got, err := svc.CurrentBalance(context.Background(), 9); err != nil || got != workers {
		t.Fatalf("concurrent balance = %d, %v; want %d", got, err, workers)
	}

	var replayWG sync.WaitGroup
	results := make(chan AwardResult, workers)
	errResults := make(chan error, workers)
	for i := 0; i < workers; i++ {
		replayWG.Add(1)
		go func() {
			defer replayWG.Done()
			result, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 9, RuleKey: "concurrent", SourceKey: "one-shared-source", OccurredAt: time.Now().UTC()})
			results <- result
			errResults <- err
		}()
	}
	replayWG.Wait()
	close(results)
	close(errResults)
	created := 0
	for result := range results {
		if !result.AlreadyApplied {
			created++
		}
	}
	for err := range errResults {
		if err != nil {
			t.Errorf("shared source replay failed: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("new shared-source entries = %d, want 1", created)
	}
	if got, _ := svc.CurrentBalance(context.Background(), 9); got != workers+1 {
		t.Fatalf("shared-source retries changed balance to %d, want %d", got, workers+1)
	}
}

func TestNegativeReversalIsAppendOnlyAndDoesNotRefundQuota(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 11)
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	awarded, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 11, RuleKey: DailyBaseRuleKey, SourceKey: "daily:11", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	updatedReward := int64(99)
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{
		Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: awarded.Entry.OccurredAt.Add(time.Second),
		Rules: []xpmodel.RewardRule{{RuleKey: DailyBaseRuleKey, Enabled: true, RewardXP: updatedReward}},
	}); err != nil {
		t.Fatalf("publish updated reward rule: %v", err)
	}
	// A valid reversal must still use the original entry, even after the
	// effective reward amount changes in a newer ruleset.
	if err := db.Model(&xpmodel.Balance{}).Where("user_id = ?", 11).Update("current_xp", 0).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostAdjustment(context.Background(), AdjustmentRequest{UserID: 11, SourceKey: "negative-balance", Delta: -1, Reason: "projection guard", ReversalOfEntryID: &awarded.Entry.ID}); !errors.Is(err, ErrNegativeBalance) {
		t.Fatalf("negative balance error = %v, want ErrNegativeBalance", err)
	}
	if err := db.Model(&xpmodel.Balance{}).Where("user_id = ?", 11).Update("current_xp", awarded.Entry.Delta).Error; err != nil {
		t.Fatal(err)
	}
	three := int64(3)
	firstReversal, err := svc.PostAdjustment(context.Background(), AdjustmentRequest{UserID: 11, SourceKey: "reverse:11:1", Delta: -three, Reason: "partial correction", ReversalOfEntryID: &awarded.Entry.ID})
	if err != nil || firstReversal.Entry.Delta != -3 || firstReversal.Entry.RuleSetVersion != awarded.Entry.RuleSetVersion {
		t.Fatalf("first reversal = %+v, %v", firstReversal, err)
	}
	replayedReversal, err := svc.PostAdjustment(context.Background(), AdjustmentRequest{UserID: 11, SourceKey: "reverse:11:1", Delta: -three, Reason: "partial correction", ReversalOfEntryID: &awarded.Entry.ID})
	if err != nil || !replayedReversal.AlreadyApplied {
		t.Fatalf("replayed reversal = %+v, %v; want idempotent result", replayedReversal, err)
	}
	secondReversal, err := svc.PostAdjustment(context.Background(), AdjustmentRequest{UserID: 11, SourceKey: "reverse:11:2", Delta: -2, Reason: "remaining correction", ReversalOfEntryID: &awarded.Entry.ID})
	if err != nil || secondReversal.Entry.Delta != -2 {
		t.Fatalf("second reversal = %+v, %v", secondReversal, err)
	}
	if _, err := svc.PostAdjustment(context.Background(), AdjustmentRequest{UserID: 11, SourceKey: "reverse:11:3", Delta: -1, Reason: "over correction", ReversalOfEntryID: &awarded.Entry.ID}); !errors.Is(err, ErrReversalLimit) {
		t.Fatalf("over reversal error = %v, want ErrReversalLimit", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 11, RuleKey: DailyBaseRuleKey, SourceKey: "daily:11:again", OccurredAt: time.Now().UTC()}); !errors.Is(err, ErrRewardLimit) {
		t.Fatalf("reward after reversal error = %v, want quota to remain consumed", err)
	}
	if got, _ := svc.CurrentBalance(context.Background(), 11); got != 0 {
		t.Fatalf("balance after full reversal = %d, want 0", got)
	}
	var entries []xpmodel.Entry
	if err := db.Order("id").Find(&entries).Error; err != nil || len(entries) != 3 {
		t.Fatalf("ledger after reversals = %d entries, %v; want original plus two reversals", len(entries), err)
	}
	if err := db.Model(&xpmodel.Entry{}).Where("id = ?", awarded.Entry.ID).Update("reason", "rewritten").Error; !errors.Is(err, xpmodel.ErrImmutableRecord) {
		t.Fatalf("ledger update error = %v, want immutable record error", err)
	}
	if err := db.Delete(&xpmodel.Entry{}, awarded.Entry.ID).Error; !errors.Is(err, xpmodel.ErrImmutableRecord) {
		t.Fatalf("ledger delete error = %v, want immutable record error", err)
	}
	if err := db.Exec("UPDATE xp_entries SET reason = ? WHERE id = ?", "bypass ORM hooks", awarded.Entry.ID).Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct ledger update")
	}
	if err := db.Exec("DELETE FROM xp_entries WHERE id = ?", awarded.Entry.ID).Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct ledger delete")
	}
}

func TestNegativeAdjustmentRequiresOriginalAward(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 12)
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	awarded, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 12, RuleKey: DailyBaseRuleKey, SourceKey: "daily:12", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.PostAdjustment(context.Background(), AdjustmentRequest{
		UserID: 12, SourceKey: "unlinked-reversal", Delta: -1, Reason: "correction without source",
	})
	if !errors.Is(err, ErrInvalidAdjustment) {
		t.Fatalf("unlinked negative adjustment error = %v, want ErrInvalidAdjustment", err)
	}
	if got, err := svc.CurrentBalance(context.Background(), 12); err != nil || got != awarded.Entry.Delta {
		t.Fatalf("balance after rejected unlinked reversal = %d, %v; want %d", got, err, awarded.Entry.Delta)
	}
	var entries int64
	if err := db.Model(&xpmodel.Entry{}).Count(&entries).Error; err != nil || entries != 1 {
		t.Fatalf("ledger rows after rejected unlinked reversal = %d, %v; want only original award", entries, err)
	}
}

func TestRuleVersionsEnablementLimitsAndShanghaiDay(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 13)
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	start := time.Date(2026, 3, 1, 15, 59, 0, 0, time.UTC) // Shanghai 23:59
	cap := int64(8)
	version, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{
		Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: start.Add(time.Minute), DailyPositiveXPLimit: &cap,
		Rules: []xpmodel.RewardRule{
			{RuleKey: DailyBaseRuleKey, Enabled: true, RewardXP: 5, DailyCountLimit: ptrInt64(1)},
			{RuleKey: "bonus", Enabled: true, RewardXP: 6, MaxPerEventXP: ptrInt64(3)},
		},
	})
	if err != nil || version != 2 {
		t.Fatalf("published ruleset version = %d, %v; want version 2", version, err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: DailyBaseRuleKey, SourceKey: "boundary:base", OccurredAt: start.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("base reward: %v", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: "bonus", SourceKey: "boundary:bonus", OccurredAt: start.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("clamped bonus reward: %v", err)
	}
	if got, _ := svc.CurrentBalance(context.Background(), 13); got != 8 {
		t.Fatalf("balance after per-event cap = %d, want 8", got)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: "bonus", SourceKey: "boundary:over-cap", OccurredAt: start.Add(3 * time.Minute)}); !errors.Is(err, ErrRewardLimit) {
		t.Fatalf("daily aggregate cap error = %v, want ErrRewardLimit", err)
	}
	var rolledBack int64
	if err := db.Model(&xpmodel.Entry{}).Where("source_key = ?", "boundary:over-cap").Count(&rolledBack).Error; err != nil || rolledBack != 0 {
		t.Fatalf("over-cap ledger rows = %d, %v; rejected award must roll back", rolledBack, err)
	}
	// Both rejected awards roll their ledger inserts back; the same Shanghai
	// calendar day remains capped even when UTC advances past 16:00.
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: DailyBaseRuleKey, SourceKey: "boundary:same-day-again", OccurredAt: start.Add(3 * time.Minute)}); !errors.Is(err, ErrRewardLimit) {
		t.Fatalf("same-day second base award error = %v, want limit", err)
	}
	nextDay := time.Date(2026, 3, 2, 16, 1, 0, 0, time.UTC)
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: DailyBaseRuleKey, SourceKey: "boundary:next-shanghai-day", OccurredAt: nextDay}); err != nil {
		t.Fatalf("next Shanghai day award: %v", err)
	}

	disabledAt := nextDay.Add(time.Hour)
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{Enabled: false, TimeZone: ShanghaiTimeZone, EffectiveAt: disabledAt, Rules: []xpmodel.RewardRule{{RuleKey: "bonus", Enabled: true, RewardXP: 1}}}); err != nil {
		t.Fatalf("publish disabled version: %v", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: "bonus", SourceKey: "disabled:bonus", OccurredAt: disabledAt.Add(time.Second)}); !errors.Is(err, ErrRuleUnavailable) {
		t.Fatalf("disabled ruleset error = %v, want ErrRuleUnavailable", err)
	}
	if err := db.Model(&xpmodel.RuleSet{}).Where("version = ?", version).Update("enabled", false).Error; !errors.Is(err, xpmodel.ErrImmutableRecord) {
		t.Fatalf("published ruleset update error = %v, want immutable record error", err)
	}
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: disabledAt.Add(2 * time.Second), Rules: []xpmodel.RewardRule{{RuleKey: "bonus", Enabled: false, RewardXP: 1}}}); err != nil {
		t.Fatalf("publish version with disabled reward rule: %v", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: 13, RuleKey: "bonus", SourceKey: "disabled:rule", OccurredAt: disabledAt.Add(3 * time.Second)}); !errors.Is(err, ErrRuleUnavailable) {
		t.Fatalf("disabled reward rule error = %v, want ErrRuleUnavailable", err)
	}
	if err := db.Exec("UPDATE xp_reward_rules SET enabled = TRUE WHERE rule_set_version = ? AND rule_key = ?", version, "bonus").Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct reward-rule update")
	}
	if err := db.Exec("DELETE FROM xp_reward_rules WHERE rule_set_version = ? AND rule_key = ?", version, "bonus").Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct reward-rule delete")
	}
	if err := db.Exec("UPDATE xp_rule_sets SET enabled = FALSE WHERE version = ?", version).Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct ruleset update")
	}
	if err := db.Exec("DELETE FROM xp_rule_sets WHERE version = ?", version).Error; err == nil {
		t.Fatal("SQLite append-only trigger accepted a direct ruleset delete")
	}
}

func TestEligibleForXPCriteriaAndDynamicLevel(t *testing.T) {
	verified := pageConfig.SecurityAndRegistration{EnableEmailVerification: true}
	base := users.EntityComplete{Id: 1, ActorType: users.ActorTypeHuman, IsFrozen: users.StatusNormal, IsActivated: 1}
	if !EligibleForXP(base, verified) {
		t.Fatal("activated human account should be eligible")
	}
	if EligibleForXP(users.EntityComplete{Id: 2, ActorType: users.ActorTypeHuman}, verified) {
		t.Fatal("unactivated account should be ineligible when email verification is enabled")
	}
	bot := base
	bot.ActorType = users.ActorTypeBot
	if EligibleForXP(bot, verified) {
		t.Fatal("bot account should be ineligible")
	}
	frozen := base
	frozen.IsFrozen = 1
	if EligibleForXP(frozen, verified) {
		t.Fatal("frozen account should be ineligible")
	}
	deleted := base
	deleted.DeletedAt = gorm.DeletedAt{Valid: true, Time: time.Now()}
	if EligibleForXP(deleted, verified) {
		t.Fatal("deleted account should be ineligible")
	}
	if got := LevelForXP(30_000, false); got != LevelSeedling {
		t.Fatalf("ineligible account level = %v, want Lv0", got)
	}
}

func TestPublishedRulesAndBalancesStartAtZeroAndAreAppendOnly(t *testing.T) {
	db := newXPTestDB(t)
	createXPUser(t, db, 17)
	svc := NewWithPolicy(db, pageConfig.SecurityAndRegistration{})
	if got, err := svc.CurrentBalance(context.Background(), 17); err != nil || got != 0 {
		t.Fatalf("initial XP = %d, %v; want zero", got, err)
	}
	if got, err := svc.CurrentLevel(context.Background(), 17); err != nil || got != LevelGreenTea {
		t.Fatalf("initial level = %v, %v; want Lv1", got, err)
	}
	if err := db.Create(&xpmodel.Balance{UserID: 17}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&xpmodel.Balance{}).Where("user_id = ?", 17).Update("current_xp", -1).Error; err == nil {
		t.Fatal("database accepted a negative XP balance")
	}
	var rule xpmodel.RewardRule
	if err := db.Where("rule_set_version = ? AND rule_key = ?", 1, DailyBaseRuleKey).Take(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if rule.RewardXP != 5 || rule.DailyCountLimit == nil || *rule.DailyCountLimit != 1 {
		t.Fatalf("seeded daily rule = %+v, want adjustable confirmed +5 and one/day", rule)
	}
}

func newXPTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:xpservice-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(12)
	}
	models := []any{&users.EntityComplete{}, &userPoints.Entity{}, &pointsRecord.Entity{}}
	models = append(models, xpmodel.Models()...)
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate XP test models: %v", err)
	}
	if err := xpmodel.EnsureAppendOnlyGuards(db); err != nil {
		t.Fatalf("install SQLite XP guards: %v", err)
	}
	if err := EnsureInitialRules(context.Background(), db); err != nil {
		t.Fatalf("seed initial XP rules: %v", err)
	}
	return db
}

func createXPUser(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	if err := db.Create(&users.EntityComplete{Id: id, Username: fmt.Sprintf("xp-user-%d", id), ActorType: users.ActorTypeHuman, IsFrozen: users.StatusNormal, Prestige: 19}).Error; err != nil {
		t.Fatalf("create XP user %d: %v", id, err)
	}
}

func ptrInt64(value int64) *int64 { return &value }
