package config

import (
	"fmt"

	"gowarp/app/assets"
)

// MaxSettingsSize accommodates two embedded loading images and their JSON encoding.
const MaxSettingsSize = 24 << 20

type LoadingSettings struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ShowProgress bool   `json:"showProgress"`
	Background   string `json:"background"`
	Progress     string `json:"progress"`
	Text         string `json:"text"`
	Logo         []byte `json:"logo,omitempty"`
	Image        []byte `json:"image,omitempty"`
}

func DefaultLoadingSettings() LoadingSettings {
	return LoadingSettings{
		ShowProgress: true,
		Background:   "#1f1f1f",
		Progress:     "#ff4b51",
		Text:         "loading",
	}
}

func (s LoadingSettings) Validate() error {
	if s.Width < 0 || s.Width > 4096 || s.Height < 0 || s.Height > 4096 {
		return fmt.Errorf("loading window dimensions must be 0..4096 (0 uses the game window size)")
	}
	if !hexColor.MatchString(s.Background) || (s.ShowProgress && !hexColor.MatchString(s.Progress)) {
		return fmt.Errorf("loading screen colors must be #RRGGBB")
	}
	if len(s.Text) > 16384 {
		return fmt.Errorf("loading text is too long")
	}
	return assets.ValidateLoadingImages(s.Logo, s.Image)
}

func (s LoadingSettings) WindowSize(width, height int) (int, int) {
	if s.Width > 0 {
		width = s.Width
	}
	if s.Height > 0 {
		height = s.Height
	}
	return width, height
}
