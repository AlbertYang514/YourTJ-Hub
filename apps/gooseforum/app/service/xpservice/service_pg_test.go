package xpservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/pageConfig"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/users"
	xpmodel "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/xp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyRewardConcurrentWritesOnPostgres(t *testing.T) {
	dsn := os.Getenv("YOURTJ_TEST_PG_URL")
	if dsn == "" {
		t.Skip("YOURTJ_TEST_PG_URL not set; skipping PostgreSQL XP service test")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schema := "xp_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA IF EXISTS \"" + schema + "\" CASCADE").Error
	})
	conn, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect isolated PostgreSQL schema: %v", err)
	}
	if sqlDB, err := conn.DB(); err == nil {
		sqlDB.SetMaxOpenConns(16)
	}
	models := []any{&users.EntityComplete{}}
	models = append(models, xpmodel.Models()...)
	if err := conn.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate XP schema on PostgreSQL: %v", err)
	}
	if err := xpmodel.EnsureAppendOnlyGuards(conn); err != nil {
		t.Fatalf("install PostgreSQL XP guards: %v", err)
	}
	if err := EnsureInitialRules(context.Background(), conn); err != nil {
		t.Fatalf("seed initial XP rules on PostgreSQL: %v", err)
	}
	if err := conn.Create(&users.EntityComplete{Username: "xp-pg-" + strconv.FormatInt(time.Now().UnixNano(), 10), ActorType: users.ActorTypeHuman, IsFrozen: users.StatusNormal}).Error; err != nil {
		t.Fatalf("create PostgreSQL XP user: %v", err)
	}
	var account users.EntityComplete
	if err := conn.Order("id DESC").Take(&account).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewWithPolicy(conn, pageConfig.SecurityAndRegistration{})
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{
		Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: time.Now().UTC(),
		Rules: []xpmodel.RewardRule{{RuleKey: "postgres_concurrent", Enabled: true, RewardXP: 1}},
	}); err != nil {
		t.Fatalf("publish PostgreSQL test rule: %v", err)
	}

	const workers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := svc.ApplyReward(context.Background(), AwardRequest{
				UserID: account.Id, RuleKey: "postgres_concurrent",
				SourceKey: fmt.Sprintf("pg-concurrent:%d", index), OccurredAt: time.Now().UTC(),
			})
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("PostgreSQL concurrent award failed: %v", err)
		}
	}
	if got, err := svc.CurrentBalance(context.Background(), account.Id); err != nil || got != workers {
		t.Fatalf("PostgreSQL XP balance = %d, %v; want %d", got, err, workers)
	}

	const replayWorkers = 16
	startReplay := make(chan struct{})
	var replayWG sync.WaitGroup
	results := make(chan AwardResult, replayWorkers)
	errorsCh := make(chan error, replayWorkers)
	for i := 0; i < replayWorkers; i++ {
		replayWG.Add(1)
		go func() {
			defer replayWG.Done()
			<-startReplay
			result, err := svc.ApplyReward(context.Background(), AwardRequest{
				UserID: account.Id, RuleKey: "postgres_concurrent", SourceKey: "pg-concurrent:shared", OccurredAt: time.Now().UTC(),
			})
			results <- result
			errorsCh <- err
		}()
	}
	close(startReplay)
	replayWG.Wait()
	close(results)
	close(errorsCh)
	created := 0
	for result := range results {
		if !result.AlreadyApplied {
			created++
		}
	}
	for err := range errorsCh {
		if err != nil {
			t.Errorf("PostgreSQL concurrent same-source replay failed: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("PostgreSQL entries created for same source = %d, want 1", created)
	}
	if got, err := svc.CurrentBalance(context.Background(), account.Id); err != nil || got != workers+1 {
		t.Fatalf("PostgreSQL XP balance after same-source replay = %d, %v; want %d", got, err, workers+1)
	}

	rollbackUserName := "xp-pg-rollback-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := conn.Create(&users.EntityComplete{Username: rollbackUserName, ActorType: users.ActorTypeHuman, IsFrozen: users.StatusNormal}).Error; err != nil {
		t.Fatalf("create PostgreSQL rollback user: %v", err)
	}
	var rollbackUser users.EntityComplete
	if err := conn.Where("username = ?", rollbackUserName).Take(&rollbackUser).Error; err != nil {
		t.Fatal(err)
	}
	limit := int64(5)
	rollbackAt := time.Now().UTC().Add(time.Hour)
	if _, err := svc.PublishRuleSet(context.Background(), RuleSetSpec{
		Enabled: true, TimeZone: ShanghaiTimeZone, DailyPositiveXPLimit: &limit, EffectiveAt: rollbackAt,
		Rules: []xpmodel.RewardRule{
			{RuleKey: "pg-rollback-base", Enabled: true, RewardXP: 5},
			{RuleKey: "pg-rollback-over", Enabled: true, RewardXP: 1},
		},
	}); err != nil {
		t.Fatalf("publish PostgreSQL rollback rules: %v", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: rollbackUser.Id, RuleKey: "pg-rollback-base", SourceKey: "pg-rollback-base", OccurredAt: rollbackAt.Add(time.Second)}); err != nil {
		t.Fatalf("PostgreSQL base award before rollback check: %v", err)
	}
	if _, err := svc.ApplyReward(context.Background(), AwardRequest{UserID: rollbackUser.Id, RuleKey: "pg-rollback-over", SourceKey: "pg-rollback-over", OccurredAt: rollbackAt.Add(2 * time.Second)}); !errors.Is(err, ErrRewardLimit) {
		t.Fatalf("PostgreSQL quota failure = %v, want ErrRewardLimit", err)
	}
	var rejectedRows int64
	if err := conn.Model(&xpmodel.Entry{}).Where("source_key = ?", "pg-rollback-over").Count(&rejectedRows).Error; err != nil || rejectedRows != 0 {
		t.Fatalf("PostgreSQL rejected award ledger rows = %d, %v; want rollback", rejectedRows, err)
	}
	var rejectedRuleUsage int64
	if err := conn.Model(&xpmodel.DailyRuleUsage{}).Where("user_id = ? AND rule_key = ?", rollbackUser.Id, "pg-rollback-over").Count(&rejectedRuleUsage).Error; err != nil || rejectedRuleUsage != 0 {
		t.Fatalf("PostgreSQL rejected award rule-usage rows = %d, %v; want rollback", rejectedRuleUsage, err)
	}
	if got, err := svc.CurrentBalance(context.Background(), rollbackUser.Id); err != nil || got != 5 {
		t.Fatalf("PostgreSQL rollback balance = %d, %v; want 5", got, err)
	}
	var committedUsage xpmodel.DailyUsage
	if err := conn.Where("user_id = ?", rollbackUser.Id).Take(&committedUsage).Error; err != nil || committedUsage.PositiveXP != 5 {
		t.Fatalf("PostgreSQL rollback daily usage = %+v, %v; want only committed 5 XP", committedUsage, err)
	}

	var entry xpmodel.Entry
	if err := conn.Where("source_key = ?", "pg-concurrent:0").Take(&entry).Error; err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name  string
		query string
		args  []any
	}{
		{name: "ledger update", query: "UPDATE xp_entries SET reason = ? WHERE id = ?", args: []any{"mutated", entry.ID}},
		{name: "ledger delete", query: "DELETE FROM xp_entries WHERE id = ?", args: []any{entry.ID}},
		{name: "ruleset update", query: "UPDATE xp_rule_sets SET enabled = FALSE WHERE version = ?", args: []any{1}},
		{name: "ruleset delete", query: "DELETE FROM xp_rule_sets WHERE version = ?", args: []any{1}},
		{name: "reward rule update", query: "UPDATE xp_reward_rules SET enabled = FALSE WHERE rule_set_version = ? AND rule_key = ?", args: []any{1, DailyBaseRuleKey}},
		{name: "reward rule delete", query: "DELETE FROM xp_reward_rules WHERE rule_set_version = ? AND rule_key = ?", args: []any{1, DailyBaseRuleKey}},
	} {
		if err := conn.Exec(mutation.query, mutation.args...).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
			t.Errorf("PostgreSQL %s error = %v; want append-only error", mutation.name, err)
		}
	}
	if err := conn.Model(&xpmodel.Balance{}).Where("user_id = ?", account.Id).Update("current_xp", -1).Error; err == nil {
		t.Fatal("PostgreSQL accepted a negative XP balance")
	}
}
