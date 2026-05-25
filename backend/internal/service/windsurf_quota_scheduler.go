package service

import (
	"context"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Windsurf 额度感知调度 ────────────────────────────────────────────────────
//
// 参考 WindsurfAPI/src/auth.js 的 quotaScore 和 windsurf-pool-7.8.0/extension/out/autoSwitcher.js
// 在 selectAccountForModelWithPlatform 中对 Windsurf 平台账号引入额度过滤与排序：
//   - quotaScore = min(dailyRemaining, weeklyRemaining)
//   - 额度耗尽（score <= exhaustThreshold 且无付费余额）→ 跳过
//   - 同优先级内按 5% 桶分组，桶高者优先；同桶按 LRU

const (
	// windsurfDefaultExhaustThreshold 默认额度耗尽阈值（%），低于此值且无余额则跳过
	windsurfDefaultExhaustThreshold = 5.0
	// windsurfDefaultMinBalanceMicros 最小可用余额（$0.10），有余额则不跳过
	windsurfDefaultMinBalanceMicros int64 = 100000
	// windsurfDefaultQuotaBucketSize 额度分桶大小（%），桶内按 LRU 轮转
	windsurfDefaultQuotaBucketSize = 5.0
	// windsurfQuotaPrefetchTTL 预取额度缓存在 context 中的有效期
	windsurfQuotaPrefetchTTL = 30 * time.Second
)

// windsurfQuotaSchedulerSettings 额度调度配置（运行时从 setting 表加载）
type windsurfQuotaSchedulerSettings struct {
	Enabled          bool    // 是否启用额度感知调度
	ExhaustThreshold float64 // 额度耗尽阈值 (%)
	MinBalanceMicros int64   // 最小可用余额 (micros)
	QuotaBucketSize  float64 // 额度分桶大小 (%)
}

// 全局缓存 windsurf 额度调度配置，避免每次请求查库
var (
	windsurfQuotaSettingsCache   atomic.Value // *cachedWindsurfQuotaSettings
	windsurfQuotaSettingsCacheMu sync.Mutex
)

type cachedWindsurfQuotaSettings struct {
	settings  windsurfQuotaSchedulerSettings
	expiresAt int64
}

const windsurfQuotaSettingsCacheTTL = 30 * time.Second

// windsurfQuotaScore 计算 Windsurf 账号的额度分数 (0-100)
// remaining = 100 - utilization
// score = min(dailyRemaining, weeklyRemaining)
func windsurfQuotaScore(usage *UsageInfo) float64 {
	if usage == nil {
		return 100 // 未获取到额度数据时不降级
	}
	daily := 100.0
	weekly := 100.0
	if usage.WindsurfDaily != nil {
		daily = 100.0 - usage.WindsurfDaily.Utilization
	}
	if usage.WindsurfWeekly != nil {
		weekly = 100.0 - usage.WindsurfWeekly.Utilization
	}
	score := math.Min(daily, weekly)
	return math.Max(0, math.Min(100, score))
}

// windsurfHasUsableBalance 检查是否有可用付费余额
func windsurfHasUsableBalance(usage *UsageInfo, minBalanceMicros int64) bool {
	if usage == nil {
		return false
	}
	return usage.WindsurfBalanceMicros >= minBalanceMicros || usage.WindsurfFlexCredits > 0
}

// windsurfQuotaBucket 将分数映射到桶号（桶越大表示额度越充足）
func windsurfQuotaBucket(score float64, bucketSize float64) int {
	if bucketSize <= 0 {
		bucketSize = windsurfDefaultQuotaBucketSize
	}
	return int(math.Floor(score / bucketSize))
}

// ─── context key 用于预取 ─────────────────────────────────────────────────────

type windsurfQuotaContextKey struct{}

type windsurfQuotaPrefetchData struct {
	scores   map[int64]float64 // accountID → quotaScore
	balances map[int64]bool    // accountID → hasUsableBalance
}

// withWindsurfQuotaPrefetch 批量预取 Windsurf 账号额度数据并注入到 context
// 避免在选择循环中对每个账号逐个查询（N+1）
func (s *GatewayService) withWindsurfQuotaPrefetch(ctx context.Context, accounts []Account, platform string) context.Context {
	if platform != PlatformWindsurf {
		return ctx
	}
	if s.accountUsageService == nil {
		return ctx
	}

	settings := s.getWindsurfQuotaSettings(ctx)
	if !settings.Enabled {
		return ctx
	}

	data := &windsurfQuotaPrefetchData{
		scores:   make(map[int64]float64, len(accounts)),
		balances: make(map[int64]bool, len(accounts)),
	}

	for i := range accounts {
		acc := &accounts[i]
		if acc.Platform != PlatformWindsurf {
			continue
		}
		usage, _ := s.accountUsageService.getWindsurfUsage(ctx, acc, "scheduling", false)
		data.scores[acc.ID] = windsurfQuotaScore(usage)
		data.balances[acc.ID] = windsurfHasUsableBalance(usage, settings.MinBalanceMicros)
	}

	return context.WithValue(ctx, windsurfQuotaContextKey{}, data)
}

// getWindsurfQuotaFromContext 从 context 中获取预取的额度数据
func getWindsurfQuotaFromContext(ctx context.Context, accountID int64) (score float64, hasBalance bool, ok bool) {
	data, _ := ctx.Value(windsurfQuotaContextKey{}).(*windsurfQuotaPrefetchData)
	if data == nil {
		return 0, false, false
	}
	score, scoreOK := data.scores[accountID]
	hasBalance, balanceOK := data.balances[accountID]
	if !scoreOK && !balanceOK {
		return 0, false, false
	}
	return score, hasBalance, true
}

// ─── 调度决策 ─────────────────────────────────────────────────────────────────

// isWindsurfAccountExhausted 判断 Windsurf 账号是否额度耗尽（不可调度）
func (s *GatewayService) isWindsurfAccountExhausted(ctx context.Context, acc *Account) bool {
	if acc.Platform != PlatformWindsurf {
		return false
	}
	if s.accountUsageService == nil {
		return false
	}
	settings := s.getWindsurfQuotaSettings(ctx)
	if !settings.Enabled {
		return false
	}

	score, hasBalance, ok := getWindsurfQuotaFromContext(ctx, acc.ID)
	if !ok {
		// fallback: 直接查询
		usage, _ := s.accountUsageService.getWindsurfUsage(ctx, acc, "scheduling", false)
		score = windsurfQuotaScore(usage)
		hasBalance = windsurfHasUsableBalance(usage, settings.MinBalanceMicros)
	}

	// 有付费余额的号即使配额耗尽也视为可用
	if hasBalance {
		return false
	}
	return score <= settings.ExhaustThreshold
}

// compareWindsurfQuotaScore 比较两个 Windsurf 账号的额度排序
// 返回 true 表示 acc 应该被选择（替代 selected）
func (s *GatewayService) compareWindsurfQuotaScore(ctx context.Context, acc *Account, selected *Account) bool {
	if acc.Platform != PlatformWindsurf || s.accountUsageService == nil {
		return false
	}
	settings := s.getWindsurfQuotaSettings(ctx)
	if !settings.Enabled {
		return false
	}

	accScore, _, accOK := getWindsurfQuotaFromContext(ctx, acc.ID)
	selScore, _, selOK := getWindsurfQuotaFromContext(ctx, selected.ID)

	if !accOK || !selOK {
		return false // 无法比较时回退到默认 LRU
	}

	accBucket := windsurfQuotaBucket(accScore, settings.QuotaBucketSize)
	selBucket := windsurfQuotaBucket(selScore, settings.QuotaBucketSize)

	// 桶高者优先（额度更充足）
	return accBucket > selBucket
}

// ─── 配置加载 ─────────────────────────────────────────────────────────────────

func (s *GatewayService) getWindsurfQuotaSettings(ctx context.Context) windsurfQuotaSchedulerSettings {
	if cached, ok := windsurfQuotaSettingsCache.Load().(*cachedWindsurfQuotaSettings); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.settings
		}
	}

	windsurfQuotaSettingsCacheMu.Lock()
	defer windsurfQuotaSettingsCacheMu.Unlock()

	// double-check
	if cached, ok := windsurfQuotaSettingsCache.Load().(*cachedWindsurfQuotaSettings); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.settings
		}
	}

	settings := windsurfQuotaSchedulerSettings{
		Enabled:          true,
		ExhaustThreshold: windsurfDefaultExhaustThreshold,
		MinBalanceMicros: windsurfDefaultMinBalanceMicros,
		QuotaBucketSize:  windsurfDefaultQuotaBucketSize,
	}

	if s.settingService != nil {
		if v, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyWindsurfQuotaSchedulingEnabled); err == nil {
			if v != "" {
				settings.Enabled = v == "true"
			}
		}
		if v, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyWindsurfQuotaExhaustThreshold); err == nil && v != "" {
			if parsed, e := strconv.ParseFloat(v, 64); e == nil && parsed >= 0 {
				settings.ExhaustThreshold = parsed
			}
		}
		if v, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyWindsurfQuotaMinBalance); err == nil && v != "" {
			if parsed, e := strconv.ParseInt(v, 10, 64); e == nil && parsed >= 0 {
				settings.MinBalanceMicros = parsed
			}
		}
		if v, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyWindsurfQuotaBucketSize); err == nil && v != "" {
			if parsed, e := strconv.ParseFloat(v, 64); e == nil && parsed > 0 {
				settings.QuotaBucketSize = parsed
			}
		}
	}

	windsurfQuotaSettingsCache.Store(&cachedWindsurfQuotaSettings{
		settings:  settings,
		expiresAt: time.Now().Add(windsurfQuotaSettingsCacheTTL).UnixNano(),
	})

	return settings
}
