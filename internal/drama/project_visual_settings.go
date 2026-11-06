package drama

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ProjectVisualSettingsInput struct {
	StyleID     *string `json:"styleId"`
	AspectRatio *string `json:"aspectRatio"`
}

type ProjectVisualSettings struct {
	StyleID     string    `json:"styleId,omitempty"`
	Style       string    `json:"style,omitempty"`
	AspectRatio string    `json:"aspectRatio,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (s *Service) UpdateProjectVisualSettings(ctx context.Context, organizationID, ownerID, id string, input ProjectVisualSettingsInput) (ProjectVisualSettings, error) {
	if _, err := uuid.Parse(id); err != nil || (input.StyleID == nil && input.AspectRatio == nil) {
		return ProjectVisualSettings{}, ErrInvalidInput
	}

	var settings ProjectVisualSettings
	if input.AspectRatio != nil {
		settings.AspectRatio = *input.AspectRatio
		if settings.AspectRatio != "9:16" && settings.AspectRatio != "16:9" {
			return ProjectVisualSettings{}, fmt.Errorf("%w：画面比例须为 9:16 或 16:9", ErrInvalidInput)
		}
	}
	if input.StyleID != nil {
		styleID := strings.TrimSpace(*input.StyleID)
		if styleID == "" || !utf8.ValidString(styleID) || strings.ContainsRune(styleID, 0) || utf8.RuneCountInString(styleID) > 80 {
			return ProjectVisualSettings{}, fmt.Errorf("%w：风格编号须为 1 到 80 个字符", ErrInvalidInput)
		}
		if s.styles == nil {
			return ProjectVisualSettings{}, ErrStyleUnavailable
		}
		style, err := s.styles.ResolveCatalogStyle(ctx, styleID)
		if err != nil {
			return ProjectVisualSettings{}, err
		}
		settings.StyleID, settings.Style = style.ID, style.Name
	}
	return s.repository.UpdateProjectVisualSettings(ctx, organizationID, ownerID, id, settings)
}
