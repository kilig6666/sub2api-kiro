package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	windsurfUsageCacheTTL  = 3 * time.Minute
	windsurfUsageErrorTTL  = 1 * time.Minute
	windsurfDefaultBaseURL = "https://server.codeium.com"
)

func (s *AccountUsageService) getWindsurfUsage(ctx context.Context, account *Account, source string, forceRefresh bool) (*UsageInfo, error) {
	if !forceRefresh {
		if cached, ok := s.getCachedWindsurfUsage(account.ID); ok {
			return cached, nil
		}
	}

	flightKey := fmt.Sprintf("windsurf-usage:%d", account.ID)
	result, err, _ := s.cache.windsurfFlight.Do(flightKey, func() (any, error) {
		if !forceRefresh {
			if cached, ok := s.getCachedWindsurfUsage(account.ID); ok {
				return cached, nil
			}
		}
		return s.fetchAndCacheWindsurfUsage(ctx, account, source)
	})
	if err != nil {
		return nil, err
	}
	return result.(*UsageInfo), nil
}

func (s *AccountUsageService) getCachedWindsurfUsage(accountID int64) (*UsageInfo, bool) {
	cached, ok := s.cache.windsurfCache.Load(accountID)
	if !ok || cached == nil {
		return nil, false
	}
	cache := cached.(*windsurfUsageCache)
	if cache.usageInfo == nil {
		return nil, false
	}
	ttl := windsurfUsageCacheTTL
	if cache.usageInfo.Error != "" {
		ttl = windsurfUsageErrorTTL
	}
	if time.Since(cache.timestamp) >= ttl {
		return nil, false
	}
	return cache.usageInfo, true
}

func (s *AccountUsageService) fetchAndCacheWindsurfUsage(ctx context.Context, account *Account, source string) (*UsageInfo, error) {
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return &UsageInfo{Source: source, Error: "missing api_key"}, nil
	}

	baseURL := windsurfDefaultBaseURL
	resp, err := s.requestWindsurfGetUserStatus(ctx, baseURL, apiKey)
	if err != nil {
		usage := &UsageInfo{Source: source, Error: err.Error()}
		s.cache.windsurfCache.Store(account.ID, &windsurfUsageCache{usageInfo: usage, timestamp: time.Now()})
		return usage, nil
	}

	now := time.Now()
	usage := s.buildWindsurfUsageInfo(resp, source, &now)

	// 获取会员期限（planStart/planEnd）
	period, _ := s.requestWindsurfGetPlanStatus(ctx, apiKey)
	if period != nil {
		usage.WindsurfPlanStart = period.PlanStart
		usage.WindsurfPlanEnd = period.PlanEnd
		// topUpStatus 中的 overageBalanceMicros 更准确
		if period.OverageBalanceMicros != 0 {
			usage.WindsurfBalanceMicros = period.OverageBalanceMicros
		}
	}

	s.cache.windsurfCache.Store(account.ID, &windsurfUsageCache{usageInfo: usage, timestamp: now})
	return usage, nil
}

type windsurfUserStatusResponse struct {
	UserStatus struct {
		PlanStatus struct {
			DailyQuotaRemainingPercent  json.Number `json:"dailyQuotaRemainingPercent"`
			WeeklyQuotaRemainingPercent json.Number `json:"weeklyQuotaRemainingPercent"`
			DailyQuotaResetAtUnix       json.Number `json:"dailyQuotaResetAtUnix"`
			WeeklyQuotaResetAtUnix      json.Number `json:"weeklyQuotaResetAtUnix"`
			AvailableFlexCredits        json.Number `json:"availableFlexCredits"`
			OverageBalanceMicros        json.Number `json:"overageBalanceMicros"`
			PlanInfo                    struct {
				PlanName string `json:"planName"`
			} `json:"planInfo"`
		} `json:"planStatus"`
	} `json:"userStatus"`
}

func (s *AccountUsageService) requestWindsurfGetUserStatus(ctx context.Context, baseURL, apiKey string) (*windsurfUserStatusResponse, error) {
	url := baseURL + "/exa.seat_management_pb.SeatManagementService/GetUserStatus"
	body := fmt.Sprintf(`{"metadata":{"apiKey":%q,"ideName":"windsurf","ideVersion":"0.0.0","extensionName":"windsurf-next","extensionVersion":"1.0.0","locale":"en"}}`, apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("api_key invalid (401)")
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("account forbidden (403)")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateString(string(respBody), 200))
	}

	var result windsurfUserStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

func (s *AccountUsageService) buildWindsurfUsageInfo(resp *windsurfUserStatusResponse, source string, now *time.Time) *UsageInfo {
	ps := resp.UserStatus.PlanStatus

	dailyRemaining := jsonNumberToFloat(ps.DailyQuotaRemainingPercent)
	weeklyRemaining := jsonNumberToFloat(ps.WeeklyQuotaRemainingPercent)
	dailyResetUnix := jsonNumberToInt64(ps.DailyQuotaResetAtUnix)
	weeklyResetUnix := jsonNumberToInt64(ps.WeeklyQuotaResetAtUnix)
	flexCredits := jsonNumberToInt64(ps.AvailableFlexCredits)
	balanceMicros := jsonNumberToInt64(ps.OverageBalanceMicros)

	usage := &UsageInfo{
		Source:                source,
		UpdatedAt:             now,
		WindsurfPlanName:      ps.PlanInfo.PlanName,
		WindsurfFlexCredits:   flexCredits,
		WindsurfBalanceMicros: balanceMicros,
	}

	// 日配额：utilization = 100 - remaining
	if dailyRemaining >= 0 {
		dailyUtil := 100.0 - dailyRemaining
		if dailyUtil < 0 {
			dailyUtil = 0
		}
		dp := &UsageProgress{Utilization: dailyUtil}
		if dailyResetUnix > 0 {
			t := time.Unix(dailyResetUnix, 0)
			dp.ResetsAt = &t
			dp.RemainingSeconds = int(t.Sub(*now).Seconds())
			if dp.RemainingSeconds < 0 {
				dp.RemainingSeconds = 0
			}
		}
		usage.WindsurfDaily = dp
	}

	// 周配额
	if weeklyRemaining >= 0 {
		weeklyUtil := 100.0 - weeklyRemaining
		if weeklyUtil < 0 {
			weeklyUtil = 0
		}
		wp := &UsageProgress{Utilization: weeklyUtil}
		if weeklyResetUnix > 0 {
			t := time.Unix(weeklyResetUnix, 0)
			wp.ResetsAt = &t
			wp.RemainingSeconds = int(t.Sub(*now).Seconds())
			if wp.RemainingSeconds < 0 {
				wp.RemainingSeconds = 0
			}
		}
		usage.WindsurfWeekly = wp
	}

	return usage
}

// windsurfPlanStatusResponse GetPlanStatus 响应
type windsurfPlanStatusResponse struct {
	PlanStart            string `json:"planStart"`
	PlanEnd              string `json:"planEnd"`
	OverageBalanceMicros int64  `json:"-"` // 从 topUpStatus 或 planStatus 解析
}

func (s *AccountUsageService) requestWindsurfGetPlanStatus(ctx context.Context, apiKey string) (*windsurfPlanStatusResponse, error) {
	url := "https://web-backend.windsurf.com/exa.seat_management_pb.SeatManagementService/GetPlanStatus"
	body := `{"includeTopUpStatus":true}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("x-auth-token", apiKey)
	req.Header.Set("x-devin-session-token", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var raw struct {
		PlanStatus struct {
			PlanStart            string      `json:"planStart"`
			PlanEnd              string      `json:"planEnd"`
			OverageBalanceMicros json.Number `json:"overageBalanceMicros"`
		} `json:"planStatus"`
		TopUpStatus struct {
			OverageBalanceMicros json.Number `json:"overageBalanceMicros"`
			BalanceMicros        json.Number `json:"balanceMicros"`
		} `json:"topUpStatus"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}

	result := &windsurfPlanStatusResponse{
		PlanStart: raw.PlanStatus.PlanStart,
		PlanEnd:   raw.PlanStatus.PlanEnd,
	}

	// 优先从 topUpStatus 获取，其次从 planStatus 获取
	if v := jsonNumberToInt64(raw.TopUpStatus.OverageBalanceMicros); v != 0 {
		result.OverageBalanceMicros = v
	} else if v := jsonNumberToInt64(raw.TopUpStatus.BalanceMicros); v != 0 {
		result.OverageBalanceMicros = v
	} else {
		result.OverageBalanceMicros = jsonNumberToInt64(raw.PlanStatus.OverageBalanceMicros)
	}

	return result, nil
}

func jsonNumberToFloat(n json.Number) float64 {
	f, _ := n.Float64()
	return f
}

func jsonNumberToInt64(n json.Number) int64 {
	i, _ := n.Int64()
	return i
}
