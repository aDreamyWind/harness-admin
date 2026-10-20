package modelgateway

import (
	"context"
	"errors"
	"fmt"
)

var ErrAlertNotFound = errors.New("模型网关预警不存在或已解除")

// ListAlerts 在查看当前月份时刷新站内预警；历史月份只读取已保存的记录。
func (s *Service) ListAlerts(ctx context.Context, query AlertQuery) (AlertPage, error) {
	currentMonth := s.now().UTC().Format("2006-01")
	if query.Month == "" {
		query.Month = currentMonth
	}
	if query.Status == "" {
		query.Status = "active"
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.OrganizationID == "" || !usageMonthPattern.MatchString(query.Month) || query.Month[:4] == "0000" ||
		(query.Status != "active" && query.Status != "all") || query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return AlertPage{}, ErrInvalidInput
	}
	if query.Month == currentMonth {
		candidates, err := s.repository.ListAlertCandidates(ctx, query.OrganizationID, query.Month)
		if err != nil {
			return AlertPage{}, fmt.Errorf("读取模型预警指标失败: %w", err)
		}
		if err := s.repository.SyncAlerts(ctx, query.OrganizationID, query.Month, evaluateAlertCandidates(candidates)); err != nil {
			return AlertPage{}, fmt.Errorf("刷新模型预警记录失败: %w", err)
		}
	}
	page, err := s.repository.ListAlerts(ctx, query)
	if err != nil {
		return AlertPage{}, fmt.Errorf("查询模型预警记录失败: %w", err)
	}
	return page, nil
}

func (s *Service) AcknowledgeAlert(ctx context.Context, input AcknowledgeAlertInput) (Alert, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.AlertID == "" {
		return Alert{}, ErrInvalidInput
	}
	item, err := s.repository.AcknowledgeAlert(ctx, input)
	if err != nil {
		if errors.Is(err, ErrAlertNotFound) {
			return Alert{}, err
		}
		return Alert{}, fmt.Errorf("确认模型预警失败: %w", err)
	}
	return item, nil
}

func (s *Service) GetAlertSettings(ctx context.Context, organizationID string) (AlertSettings, error) {
	if organizationID == "" {
		return AlertSettings{}, ErrInvalidInput
	}
	settings, err := s.repository.GetAlertSettings(ctx, organizationID)
	if err != nil {
		return AlertSettings{}, fmt.Errorf("读取模型预警设置失败: %w", err)
	}
	return settings, nil
}

func (s *Service) UpdateAlertSettings(ctx context.Context, input UpdateAlertSettingsInput) (AlertSettings, error) {
	if input.OrganizationID == "" || input.ActorID == "" {
		return AlertSettings{}, ErrInvalidInput
	}
	settings, err := s.repository.UpdateAlertSettings(ctx, input)
	if err != nil {
		return AlertSettings{}, fmt.Errorf("修改模型预警设置失败: %w", err)
	}
	return settings, nil
}

// RefreshEnabledAlerts 只计算已开启后台刷新的企业，始终使用当前 UTC 月。
func (s *Service) RefreshEnabledAlerts(ctx context.Context) error {
	organizations, err := s.repository.ListAutoRefreshOrganizations(ctx)
	if err != nil {
		return fmt.Errorf("查询启用后台预警的企业失败: %w", err)
	}
	month := s.now().UTC().Format("2006-01")
	var refreshErrors []error
	for _, organizationID := range organizations {
		if err := ctx.Err(); err != nil {
			return err
		}
		settings, err := s.repository.GetAlertSettings(ctx, organizationID)
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("复核企业 %s 的后台预警开关失败: %w", organizationID, err))
			continue
		}
		if !settings.AutoRefreshEnabled {
			continue
		}
		candidates, err := s.repository.ListAlertCandidates(ctx, organizationID, month)
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("读取企业 %s 的模型预警指标失败: %w", organizationID, err))
			continue
		}
		if err := s.repository.SyncAlerts(ctx, organizationID, month, evaluateAlertCandidates(candidates)); err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("刷新企业 %s 的模型预警记录失败: %w", organizationID, err))
		}
	}
	return errors.Join(refreshErrors...)
}

func evaluateAlertCandidates(candidates []AlertCandidate) []AlertSignal {
	signals := make([]AlertSignal, 0, len(candidates))
	for _, candidate := range candidates {
		severity := ""
		switch candidate.Kind {
		case "quota":
			if candidate.TokenLimit <= 0 {
				continue
			}
			if candidate.UsedTokens >= candidate.TokenLimit {
				severity = "critical"
			} else if candidate.UsedTokens >= candidate.TokenLimit-candidate.TokenLimit/5 {
				severity = "warning"
			}
		case "unknown_usage":
			// 成功调用至少 5 次用量未知，且占成功调用的至少 20%，才生成站内预警。
			if candidate.UnknownUsageCalls >= 5 && candidate.CallCount > 0 && candidate.UnknownUsageCalls >= (candidate.CallCount+4)/5 {
				severity = "warning"
			}
		}
		if severity != "" {
			signals = append(signals, AlertSignal{AlertCandidate: candidate, Severity: severity})
		}
	}
	return signals
}
