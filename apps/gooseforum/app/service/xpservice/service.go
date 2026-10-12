package xpservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/pageConfig"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/users"
	xpmodel "github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/xp"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/hotdataserve"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DailyBaseRuleKey = "daily_base"
	ShanghaiTimeZone = "Asia/Shanghai"
)

var (
	ErrIneligible        = errors.New("account is not eligible for XP")
	ErrRuleUnavailable   = errors.New("XP reward rule unavailable")
	ErrRewardLimit       = errors.New("XP reward limit exceeded")
	ErrInvalidRule       = errors.New("invalid XP reward rule")
	ErrInvalidAdjustment = errors.New("invalid XP adjustment")
	ErrSourceKeyConflict = errors.New("XP source key already used for a different operation")
	ErrNegativeBalance   = errors.New("XP balance cannot become negative")
	ErrReversalLimit     = errors.New("XP reversal exceeds original award")
)

type Service struct {
	db     *gorm.DB
	policy func() pageConfig.SecurityAndRegistration
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, policy: hotdataserve.GetSecuritySettingsConfigCache}
}

// NewWithPolicyProvider is useful for jobs whose policy snapshot is already
// loaded and for tests. Production callers should normally use New.
func NewWithPolicyProvider(db *gorm.DB, policy func() pageConfig.SecurityAndRegistration) *Service {
	return &Service{db: db, policy: policy}
}

func NewWithPolicy(db *gorm.DB, policy pageConfig.SecurityAndRegistration) *Service {
	return NewWithPolicyProvider(db, func() pageConfig.SecurityAndRegistration { return policy })
}

func EligibleForXP(user users.EntityComplete, security pageConfig.SecurityAndRegistration) bool {
	if user.Id == 0 || user.ActorType != users.ActorTypeHuman || user.IsFrozen != users.StatusNormal || user.DeletedAt.Valid {
		return false
	}
	return !security.EnableEmailVerification || user.IsActivated == 1
}

type AwardRequest struct {
	UserID     uint64
	RuleKey    string
	SourceKey  string
	OccurredAt time.Time
}

type AwardResult struct {
	Entry          xpmodel.Entry
	AlreadyApplied bool
}

func (s *Service) ApplyReward(ctx context.Context, request AwardRequest) (AwardResult, error) {
	if s == nil || s.db == nil || s.policy == nil {
		return AwardResult{}, errors.New("XP service is not configured")
	}
	if request.UserID == 0 || strings.TrimSpace(request.SourceKey) == "" || len(request.SourceKey) > 255 || strings.TrimSpace(request.RuleKey) == "" || len(request.RuleKey) > 64 || request.OccurredAt.IsZero() {
		return AwardResult{}, ErrInvalidRule
	}
	request.SourceKey = strings.TrimSpace(request.SourceKey)
	request.RuleKey = strings.TrimSpace(request.RuleKey)
	var result AwardResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		balance, err := lockBalance(tx, request.UserID)
		if err != nil {
			return err
		}

		var existing xpmodel.Entry
		err = tx.Where("source_key = ?", request.SourceKey).Take(&existing).Error
		if err == nil {
			if existing.UserID != request.UserID || existing.RuleKey != request.RuleKey || existing.Delta <= 0 {
				return ErrSourceKeyConflict
			}
			result = AwardResult{Entry: existing, AlreadyApplied: true}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if _, err := lockEligibleAccount(tx, request.UserID, s.policy()); err != nil {
			return err
		}

		localTime := request.OccurredAt.In(shanghaiLocation())
		var ruleSet xpmodel.RuleSet
		err = tx.Where("effective_at <= ?", request.OccurredAt).Order("effective_at DESC").Take(&ruleSet).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRuleUnavailable
		}
		if err != nil {
			return err
		}
		if !ruleSet.Enabled || ruleSet.TimeZone != ShanghaiTimeZone {
			return ErrRuleUnavailable
		}
		var rule xpmodel.RewardRule
		if err := tx.Where("rule_set_version = ? AND rule_key = ? AND enabled = ?", ruleSet.Version, request.RuleKey, true).Take(&rule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRuleUnavailable
		} else if err != nil {
			return err
		}
		if rule.RewardXP <= 0 {
			return ErrInvalidRule
		}
		amount := rule.RewardXP
		if rule.MaxPerEventXP != nil && amount > *rule.MaxPerEventXP {
			amount = *rule.MaxPerEventXP
		}
		if amount <= 0 || balance.CurrentXP > math.MaxInt64-amount {
			return ErrInvalidRule
		}

		entry := xpmodel.Entry{
			UserID: request.UserID, SourceKey: request.SourceKey,
			RuleSetVersion: ruleSet.Version, RuleKey: request.RuleKey,
			Delta: amount, OccurredAt: request.OccurredAt.UTC(),
		}
		insert := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_key"}}, DoNothing: true}).Create(&entry)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			if err := tx.Where("source_key = ?", request.SourceKey).Take(&existing).Error; err != nil {
				return err
			}
			if existing.UserID != request.UserID || existing.RuleKey != request.RuleKey || existing.Delta <= 0 {
				return ErrSourceKeyConflict
			}
			result = AwardResult{Entry: existing, AlreadyApplied: true}
			return nil
		}

		usageDate := localTime.Format("2006-01-02")
		ruleUsage, err := getDailyRuleUsage(tx, request.UserID, request.RuleKey, usageDate)
		if err != nil {
			return err
		}
		if rule.DailyCountLimit != nil && ruleUsage.UsedCount >= *rule.DailyCountLimit {
			return ErrRewardLimit
		}
		if ruleUsage.UsedCount == math.MaxInt64 {
			return ErrInvalidRule
		}
		ruleUsage.UsedCount++
		if err := saveUsage(tx, &ruleUsage); err != nil {
			return err
		}

		dailyUsage, err := getDailyUsage(tx, request.UserID, usageDate)
		if err != nil {
			return err
		}
		if amount > math.MaxInt64-dailyUsage.PositiveXP {
			return ErrInvalidRule
		}
		if ruleSet.DailyPositiveXPLimit != nil && amount > *ruleSet.DailyPositiveXPLimit-dailyUsage.PositiveXP {
			return ErrRewardLimit
		}
		dailyUsage.PositiveXP += amount
		if err := saveUsage(tx, &dailyUsage); err != nil {
			return err
		}

		balance.CurrentXP += amount
		if err := tx.Model(&xpmodel.Balance{}).Where("user_id = ?", request.UserID).UpdateColumn("current_xp", balance.CurrentXP).Error; err != nil {
			return err
		}
		result.Entry = entry
		return nil
	})
	return result, err
}

type AdjustmentRequest struct {
	UserID            uint64
	SourceKey         string
	Delta             int64
	Reason            string
	OccurredAt        time.Time
	ReversalOfEntryID *uint64 // Required: the original positive award being reversed.
}

// PostAdjustment writes a negative reversal. It never consumes or refunds
// positive-award quota. The required original-award reference caps cumulative
// reversals at the amount that was actually granted.
func (s *Service) PostAdjustment(ctx context.Context, request AdjustmentRequest) (AwardResult, error) {
	if s == nil || s.db == nil || request.UserID == 0 || strings.TrimSpace(request.SourceKey) == "" || len(request.SourceKey) > 255 || request.Delta >= 0 || request.Delta == math.MinInt64 || strings.TrimSpace(request.Reason) == "" || request.ReversalOfEntryID == nil || *request.ReversalOfEntryID == 0 {
		return AwardResult{}, ErrInvalidAdjustment
	}
	if request.OccurredAt.IsZero() {
		request.OccurredAt = time.Now().UTC()
	}
	request.SourceKey = strings.TrimSpace(request.SourceKey)
	request.Reason = strings.TrimSpace(request.Reason)
	var result AwardResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		balance, err := lockBalance(tx, request.UserID)
		if err != nil {
			return err
		}
		var existing xpmodel.Entry
		if err := tx.Where("source_key = ?", request.SourceKey).Take(&existing).Error; err == nil {
			if existing.UserID != request.UserID || existing.Delta != request.Delta || !sameID(existing.ReversalOfEntry, request.ReversalOfEntryID) {
				return ErrSourceKeyConflict
			}
			result = AwardResult{Entry: existing, AlreadyApplied: true}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := accountExists(tx, request.UserID); err != nil {
			return err
		}
		var original xpmodel.Entry
		if request.ReversalOfEntryID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", *request.ReversalOfEntryID).Take(&original).Error; err != nil {
				return err
			}
			if original.UserID != request.UserID || original.Delta <= 0 {
				return ErrReversalLimit
			}
			var alreadyReversed int64
			if err := tx.Model(&xpmodel.Entry{}).Select("COALESCE(SUM(-delta), 0)").Where("reversal_of_entry_id = ? AND delta < 0", original.ID).Scan(&alreadyReversed).Error; err != nil {
				return err
			}
			if alreadyReversed > original.Delta || -request.Delta > original.Delta-alreadyReversed {
				return ErrReversalLimit
			}
		}
		if balance.CurrentXP < -request.Delta {
			return ErrNegativeBalance
		}
		entry := xpmodel.Entry{
			UserID: request.UserID, SourceKey: request.SourceKey, RuleKey: "adjustment",
			Delta: request.Delta, OccurredAt: request.OccurredAt.UTC(), Reason: request.Reason,
			ReversalOfEntry: request.ReversalOfEntryID,
		}
		if request.ReversalOfEntryID != nil {
			entry.RuleKey = original.RuleKey
			entry.RuleSetVersion = original.RuleSetVersion
		}
		insert := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_key"}}, DoNothing: true}).Create(&entry)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			if err := tx.Where("source_key = ?", request.SourceKey).Take(&existing).Error; err != nil {
				return err
			}
			if existing.UserID != request.UserID || existing.Delta != request.Delta || !sameID(existing.ReversalOfEntry, request.ReversalOfEntryID) {
				return ErrSourceKeyConflict
			}
			result = AwardResult{Entry: existing, AlreadyApplied: true}
			return nil
		}
		balance.CurrentXP += request.Delta
		if err := tx.Model(&xpmodel.Balance{}).Where("user_id = ?", request.UserID).UpdateColumn("current_xp", balance.CurrentXP).Error; err != nil {
			return err
		}
		result.Entry = entry
		return nil
	})
	return result, err
}

func (s *Service) CurrentBalance(ctx context.Context, userID uint64) (int64, error) {
	var balance xpmodel.Balance
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&balance).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return balance.CurrentXP, err
}

func (s *Service) CurrentLevel(ctx context.Context, userID uint64) (Level, error) {
	var user users.EntityComplete
	if err := s.db.WithContext(ctx).Where("id = ?", userID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LevelSeedling, nil
		}
		return LevelSeedling, err
	}
	balance, err := s.CurrentBalance(ctx, userID)
	if err != nil {
		return LevelSeedling, err
	}
	return LevelForXP(balance, EligibleForXP(user, s.policy())), nil
}

type RuleSetSpec struct {
	Enabled              bool
	TimeZone             string
	DailyPositiveXPLimit *int64
	EffectiveAt          time.Time
	Rules                []xpmodel.RewardRule
}

// PublishRuleSet appends a new effective-dated ruleset. Published versions and
// their rules are immutable; disabling a policy requires publishing a disabled
// version instead of editing history.
func (s *Service) PublishRuleSet(ctx context.Context, spec RuleSetSpec) (uint64, error) {
	if spec.TimeZone == "" {
		spec.TimeZone = ShanghaiTimeZone
	}
	if spec.TimeZone != ShanghaiTimeZone || spec.EffectiveAt.IsZero() || len(spec.Rules) == 0 || (spec.DailyPositiveXPLimit != nil && *spec.DailyPositiveXPLimit < 0) {
		return 0, ErrInvalidRule
	}
	seen := make(map[string]struct{}, len(spec.Rules))
	for _, rule := range spec.Rules {
		if strings.TrimSpace(rule.RuleKey) == "" || len(rule.RuleKey) > 64 || rule.RewardXP <= 0 || (rule.MaxPerEventXP != nil && *rule.MaxPerEventXP <= 0) || (rule.DailyCountLimit != nil && *rule.DailyCountLimit < 0) {
			return 0, ErrInvalidRule
		}
		if _, exists := seen[rule.RuleKey]; exists {
			return 0, ErrInvalidRule
		}
		seen[rule.RuleKey] = struct{}{}
	}
	var version uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Migration v33 guarantees the immutable version-1 anchor. Serializing
		// publishers through it prevents two administrators from allocating the
		// same next version concurrently.
		var anchor xpmodel.RuleSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("version = ?", 1).Take(&anchor).Error; err != nil {
			return fmt.Errorf("lock XP ruleset version anchor: %w", err)
		}
		var latest xpmodel.RuleSet
		err := tx.Order("version DESC").Take(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if latest.Version > 0 && !spec.EffectiveAt.After(latest.EffectiveAt) {
			return ErrInvalidRule
		}
		version = latest.Version + 1
		set := xpmodel.RuleSet{Version: version, Enabled: spec.Enabled, TimeZone: spec.TimeZone, DailyPositiveXPLimit: spec.DailyPositiveXPLimit, EffectiveAt: spec.EffectiveAt.UTC()}
		if err := tx.Select("*").Create(&set).Error; err != nil {
			return err
		}
		for _, rule := range spec.Rules {
			rule.RuleSetVersion = version
			rule.RuleKey = strings.TrimSpace(rule.RuleKey)
			if err := tx.Select("*").Create(&rule).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return version, err
}

// EnsureInitialRules seeds only the confirmed daily +5 XP policy. Other reward
// values and daily caps remain unset until the team decides them.
func EnsureInitialRules(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&xpmodel.RuleSet{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		set := xpmodel.RuleSet{Version: 1, Enabled: true, TimeZone: ShanghaiTimeZone, EffectiveAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
		if err := tx.Create(&set).Error; err != nil {
			return err
		}
		five := int64(5)
		one := int64(1)
		return tx.Create(&xpmodel.RewardRule{RuleSetVersion: 1, RuleKey: DailyBaseRuleKey, Enabled: true, RewardXP: five, MaxPerEventXP: &five, DailyCountLimit: &one}).Error
	})
}

func lockBalance(tx *gorm.DB, userID uint64) (xpmodel.Balance, error) {
	zero := xpmodel.Balance{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoNothing: true}).Create(&zero).Error; err != nil {
		return xpmodel.Balance{}, err
	}
	var balance xpmodel.Balance
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).Take(&balance).Error
	return balance, err
}

func lockEligibleAccount(tx *gorm.DB, userID uint64, security pageConfig.SecurityAndRegistration) (users.EntityComplete, error) {
	var user users.EntityComplete
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return user, ErrIneligible
	}
	if err != nil {
		return user, err
	}
	if !EligibleForXP(user, security) {
		return user, ErrIneligible
	}
	return user, nil
}

func accountExists(tx *gorm.DB, userID uint64) error {
	var id uint64
	err := tx.Unscoped().Model(&users.EntityComplete{}).Select("id").Where("id = ?", userID).Take(&id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrIneligible
	}
	return err
}

func getDailyUsage(tx *gorm.DB, userID uint64, date string) (xpmodel.DailyUsage, error) {
	row := xpmodel.DailyUsage{UserID: userID, UsageDate: date}
	err := tx.Where("user_id = ? AND usage_date = ?", userID, date).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, nil
	}
	return row, err
}

func getDailyRuleUsage(tx *gorm.DB, userID uint64, ruleKey, date string) (xpmodel.DailyRuleUsage, error) {
	row := xpmodel.DailyRuleUsage{UserID: userID, RuleKey: ruleKey, UsageDate: date}
	err := tx.Where("user_id = ? AND rule_key = ? AND usage_date = ?", userID, ruleKey, date).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, nil
	}
	return row, err
}

func saveUsage(tx *gorm.DB, row any) error {
	result := tx.Save(row)
	return result.Error
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation(ShanghaiTimeZone)
	if err != nil {
		panic(fmt.Sprintf("load required timezone %s: %v", ShanghaiTimeZone, err))
	}
	return location
}

func sameID(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
