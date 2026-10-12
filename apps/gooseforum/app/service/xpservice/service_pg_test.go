package xpservice

import (
	"context"
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

	var entry xpmodel.Entry
	if err := conn.Where("source_key = ?", "pg-concurrent:0").Take(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("UPDATE xp_entries SET reason = ? WHERE id = ?", "mutated", entry.ID).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Fatalf("PostgreSQL ledger update error = %v; want append-only error", err)
	}
	if err := conn.Exec("DELETE FROM xp_entries WHERE id = ?", entry.ID).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Fatalf("PostgreSQL ledger delete error = %v; want append-only error", err)
	}
	if err := conn.Exec("UPDATE xp_reward_rules SET enabled = FALSE WHERE rule_set_version = ? AND rule_key = ?", 1, DailyBaseRuleKey).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Fatalf("PostgreSQL published-rule update error = %v; want append-only error", err)
	}
	if err := conn.Model(&xpmodel.Balance{}).Where("user_id = ?", account.Id).Update("current_xp", -1).Error; err == nil {
		t.Fatal("PostgreSQL accepted a negative XP balance")
	}
}
