package i18n

import (
	"strings"
	"testing"
)

func TestCSVTranslationAndEnglishFallback(t *testing.T) {
	catalog, err := parse([]byte("\ufeffkey,en,ko\nlanguage.name,English,한국어\nhello,Hello %s,안녕하세요 %s\nmissing,English fallback,\nquoted,\"First, line\nSecond line\",\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ language, key, want string }{
		{"ko", "hello", "안녕하세요 %s"},
		{"ko", "missing", "English fallback"},
		{"fr", "hello", "Hello %s"},
		{"en", "quoted", "First, line\nSecond line"},
	} {
		if got := catalog.text(test.language, test.key); got != test.want {
			t.Errorf("%s/%s = %q, want %q", test.language, test.key, got, test.want)
		}
	}
}

func TestCSVRejectsAmbiguousOrMissingEnglish(t *testing.T) {
	for _, csv := range []string{
		"key,ko\nx,값\n",
		"key,en,en\nx,a,b\n",
		"key,en,ko\nx,One,하나\nx,Two,둘\n",
		"key,en,ko\nx,,값\n",
	} {
		if _, err := parse([]byte(csv)); err == nil {
			t.Fatalf("accepted invalid catalog: %q", csv)
		}
	}
}

func TestEmbeddedCatalogDefaultsToEnglishAndChangesLanguage(t *testing.T) {
	previous := LanguageCode()
	t.Cleanup(func() { SetLanguage(previous) })
	SetLanguage("en")
	if Tr("action.open_project") != "Open SB3 file" {
		t.Fatal("English UI translation missing")
	}
	if !SetLanguage("ko") || Tr("action.open_project") != "SB3 파일 열기" {
		t.Fatal("Korean UI translation missing")
	}
	if SetLanguage("unknown") || LanguageCode() != "ko" {
		t.Fatal("unknown language replaced the selected language")
	}
	if got := Tr("status.saved", "game.exe"); !strings.Contains(got, "game.exe") {
		t.Fatalf("formatted translation lost its argument: %q", got)
	}
}
