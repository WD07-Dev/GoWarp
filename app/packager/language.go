package packager

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gowarp/app/i18n"
)

type preferences struct {
	Language string `json:"language"`
}

func loadLanguage(path string) error {
	i18n.SetLanguage("en")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved preferences
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	i18n.SetLanguage(saved.Language)
	return nil
}

func (a *application) setLanguage(code string) {
	valid := false
	for _, language := range i18n.Languages() {
		if language.Code == code {
			valid = true
			break
		}
	}
	if !valid {
		return
	}
	if a.languagePath != "" {
		data, err := json.Marshal(preferences{Language: code})
		if err == nil {
			err = os.MkdirAll(filepath.Dir(a.languagePath), 0700)
		}
		if err == nil {
			err = os.WriteFile(a.languagePath, data, 0600)
		}
		if err != nil {
			a.errorText = err.Error()
			a.dirty = true
			return
		}
	}
	i18n.SetLanguage(code)
	a.closeDropdown()
	a.editor = nil
	a.keyboardFocus = -1
	a.status = i18n.Tr("language.changed")
	a.dirty = true
}

func (a *application) renderLanguage() {
	languages := i18n.Languages()
	names := make([]string, len(languages))
	selected := 0
	for i, language := range languages {
		names[i] = language.Name
		if language.Code == i18n.LanguageCode() {
			selected = i
		}
	}
	a.choice(250, 18, 150, names, selected, func(index int) {
		a.setLanguage(languages[index].Code)
	})
}
