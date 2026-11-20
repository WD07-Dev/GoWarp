package config

import (
	"fmt"

	"gowarp/app/assets"
)

// MaxSettingsSize accommodates two embedded loading images and their JSON encoding.
const MaxSettingsSize = 24 << 20

type LoadingSettings struct {
	Background string `json:"background"`
	Progress   string `json:"progress"`
	Text       string `json:"text"`
	Logo       []byte `json:"logo,omitempty"`
	Image      []byte `json:"image,omitempty"`
}

func DefaultLoadingSettings() LoadingSettings {
	return LoadingSettings{
		Background: "#1f1f1f",
		Progress:   "#ff4b51",
		Text:       "loading",
	}
}

func (s LoadingSettings) Validate() error {
	if !hexColor.MatchString(s.Background) || !hexColor.MatchString(s.Progress) {
		return fmt.Errorf("loading screen colors must be #RRGGBB")
	}
	if len(s.Text) > 16384 {
		return fmt.Errorf("loading text is too long")
	}
	return assets.ValidateLoadingImages(s.Logo, s.Image)
}
