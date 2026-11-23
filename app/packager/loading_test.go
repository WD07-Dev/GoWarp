package packager

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"gowarp/app/config"
)

func TestLoadingFormPreservesAndRemovesImages(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 12, 6))); err != nil {
		t.Fatal(err)
	}
	s := config.Default(nil)
	s.Loading.Logo, s.Loading.Image = data.Bytes(), data.Bytes()
	f := newForm(s, nil)
	if f.loadingLogo == nil || f.loadingImage == nil {
		t.Fatal("loading thumbnails missing")
	}
	for i := range f.fields {
		switch f.fields[i].key {
		case "loading.background":
			f.fields[i].value = "#123456"
		case "loading.progress":
			f.fields[i].value = "#abcdef"
		case "loading.text":
			f.fields[i].value = "Ready soon"
		}
	}
	got, err := f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Loading.Background != "#123456" || got.Loading.Progress != "#abcdef" || got.Loading.Text != "Ready soon" || !bytes.Equal(got.Loading.Logo, data.Bytes()) || !bytes.Equal(got.Loading.Image, data.Bytes()) {
		t.Fatal("loading form binding lost")
	}
	f.setLoadingImage("loading.logo", nil, nil)
	f.setLoadingImage("loading.image", nil, nil)
	got, err = f.settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Loading.Logo != nil || got.Loading.Image != nil || f.loadingLogo != nil || f.loadingImage != nil {
		t.Fatal("removed loading image retained")
	}
}

func TestLoadingTabFitsMinimumWindow(t *testing.T) {
	a := renderedApplication(t)
	w, h := a.layoutScale(760, 540, 1)
	a.pixels = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	a.section = 5
	a.render()
	if !fieldInputVisible(a, "loading.background") || !fieldInputVisible(a, "loading.progress") {
		t.Fatal("loading colors missing")
	}
	for _, hit := range a.hits[3 : 3+len(sections)] {
		if hit.rect.Max.Y > a.height-128 {
			t.Fatal("section tabs exceed sidebar")
		}
	}
	a.scroll = 10000
	a.render()
	if a.scroll == 0 {
		t.Fatal("loading image controls cannot be reached")
	}
}

func TestLoadingProgressCanBeHidden(t *testing.T) {
	a := renderedApplication(t)
	a.section = 5
	found := false
	for i := range a.form.fields {
		f := &a.form.fields[i]
		switch f.key {
		case "loading.showProgress":
			f.value = "false"
			found = true
		case "loading.text":
			f.value = "Please wait"
		}
	}
	if !found {
		t.Fatal("loading progress visibility setting is missing")
	}
	s, err := a.form.settings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Loading.Text != "Please wait" || s.Loading.ShowProgress {
		t.Fatal("hiding progress changed the custom loading message")
	}
	for _, i := range a.form.visibleFields(5) {
		if a.form.fields[i].key == "loading.progress" {
			t.Fatal("hidden progress still exposes its color setting")
		}
	}
}
