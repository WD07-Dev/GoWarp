// Package i18n provides the UI strings embedded from translations.csv.
package i18n

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"gowarp/app/assets"
)

type Language struct {
	Code string
	Name string
}

type catalog struct {
	languages []Language
	entries   map[string]map[string]string
}

func parse(data []byte) (*catalog, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	if len(header) < 2 || header[0] != "key" {
		return nil, fmt.Errorf("translation CSV must start with key and language columns")
	}
	c := &catalog{entries: make(map[string]map[string]string)}
	seen := map[string]bool{}
	for _, code := range header[1:] {
		if code == "" || strings.TrimSpace(code) != code || seen[code] {
			return nil, fmt.Errorf("invalid or duplicate language column %q", code)
		}
		seen[code] = true
		c.languages = append(c.languages, Language{Code: code, Name: code})
	}
	if !seen["en"] {
		return nil, fmt.Errorf("translation CSV requires an en column")
	}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		key := row[0]
		if key == "" || c.entries[key] != nil {
			return nil, fmt.Errorf("empty or duplicate translation key %q", key)
		}
		entry := make(map[string]string)
		for i, code := range header[1:] {
			entry[code] = row[i+1]
		}
		if entry["en"] == "" {
			return nil, fmt.Errorf("translation %q requires English text", key)
		}
		c.entries[key] = entry
	}
	for i := range c.languages {
		if name := c.entries["language.name"][c.languages[i].Code]; name != "" {
			c.languages[i].Name = name
		}
	}
	return c, nil
}

func (c *catalog) text(language, key string) string {
	entry := c.entries[key]
	if value := entry[language]; value != "" {
		return value
	}
	if value := entry["en"]; value != "" {
		return value
	}
	return key
}

var messages = func() *catalog {
	c, err := parse(assets.Translations)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded translations: %v", err))
	}
	return c
}()

var selected atomic.Value

func LanguageCode() string {
	if language := selected.Load(); language != nil {
		return language.(string)
	}
	return "en"
}

func Languages() []Language {
	return append([]Language(nil), messages.languages...)
}

func SetLanguage(code string) bool {
	for _, language := range messages.languages {
		if language.Code == code {
			selected.Store(code)
			return true
		}
	}
	return false
}

func Tr(key string, args ...any) string {
	text := messages.text(LanguageCode(), key)
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}
