package packager

import (
	"context"
	"image"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"gowarp/app/assets"
	"gowarp/app/i18n"
	"gowarp/app/ui"
)

func (a *application) loadLoadingImage(key, path string) {
	loading := a.form.base.Loading
	a.job(i18n.Tr("job.loading_image"), func(ctx context.Context) result {
		data, err := assets.LoadLoadingImage(path)
		if err != nil {
			return result{err: err}
		}
		if key == "loading.logo" {
			loading.Logo = data
		} else {
			loading.Image = data
		}
		if err := assets.ValidateLoadingImages(loading.Logo, loading.Image); err != nil {
			return result{err: err}
		}
		decoded, err := assets.DecodeLoadingImage(data)
		if err != nil {
			return result{err: err}
		}
		if err := ctx.Err(); err != nil {
			return result{err: err}
		}
		return result{loadingKey: key, loadingData: data, loadingImage: decoded, status: i18n.Tr("status.image_loaded", filepath.Base(path))}
	})
}

func (f *form) setLoadingImage(key string, data []byte, decoded *assets.LoadingImage) {
	if key == "loading.logo" {
		f.base.Loading.Logo, f.loadingLogo = data, decoded
	} else if key == "loading.image" {
		f.base.Loading.Image, f.loadingImage = data, decoded
	}
}

func (a *application) renderLoadingImage(rect image.Rectangle, key string) {
	x, y := rect.Min.X+12, rect.Min.Y+36
	a.button(x, y, 116, i18n.Tr("action.choose_image"), func() { a.choose(key) })
	preview := a.form.loadingLogo
	if key == "loading.image" {
		preview = a.form.loadingImage
	}
	if preview == nil {
		a.label(x+132, y+23, i18n.Tr("image.none"), ui.Muted)
		return
	}
	a.button(x+128, y, 116, i18n.Tr("image.remove"), func() {
		a.form.setLoadingImage(key, nil, nil)
	})
	box := a.pixelRect(image.Rect(rect.Max.X-92, rect.Min.Y+14, rect.Max.X-16, rect.Min.Y+70))
	ui.Fill(a.pixels, box, ui.Field)
	draw.CatmullRom.Scale(a.pixels, fitLoadingImage(box, preview.Bounds()), preview, preview.Bounds(), draw.Over, nil)
}

func fitLoadingImage(box, source image.Rectangle) image.Rectangle {
	w, h := box.Dx(), box.Dy()
	if source.Dx()*h > source.Dy()*w {
		h = max(1, source.Dy()*w/source.Dx())
	} else {
		w = max(1, source.Dx()*h/source.Dy())
	}
	x, y := box.Min.X+(box.Dx()-w)/2, box.Min.Y+(box.Dy()-h)/2
	return image.Rect(x, y, x+w, y+h)
}

func loadingPreviewColor(value string, fallback color.Color) color.Color {
	if len(value) != 7 || value[0] != '#' {
		return fallback
	}
	n, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return fallback
	}
	return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
}

func (a *application) renderLoadingPreview(rect image.Rectangle) {
	background, progress, text := a.form.base.Loading.Background, a.form.base.Loading.Progress, a.form.base.Loading.Text
	showProgress := a.form.base.Loading.ShowProgress
	for _, f := range a.form.fields {
		switch f.key {
		case "loading.background":
			background = f.value
		case "loading.progress":
			progress = f.value
		case "loading.text":
			text = f.value
		case "loading.showProgress":
			showProgress = f.value == "true"
		}
	}
	a.fill(rect, loadingPreviewColor(background, ui.Background))
	box := a.pixelRect(rect)
	if source := a.form.loadingImage; source != nil {
		crop := source.Bounds()
		if crop.Dx()*box.Dy() > crop.Dy()*box.Dx() {
			width := max(1, crop.Dy()*box.Dx()/box.Dy())
			crop.Min.X += (crop.Dx() - width) / 2
			crop.Max.X = crop.Min.X + width
		} else {
			height := max(1, crop.Dx()*box.Dy()/box.Dx())
			crop.Min.Y += (crop.Dy() - height) / 2
			crop.Max.Y = crop.Min.Y + height
		}
		draw.CatmullRom.Scale(a.pixels, box, source, crop, draw.Over, nil)
	}
	panelTop := rect.Min.Y + 20
	if logo := a.form.loadingLogo; logo != nil {
		logoBox := a.pixelRect(image.Rect(rect.Min.X+24, rect.Min.Y+4, rect.Max.X-24, rect.Min.Y+64))
		draw.CatmullRom.Scale(a.pixels, fitLoadingImage(logoBox, logo.Bounds()), logo, logo.Bounds(), draw.Over, nil)
		panelTop = rect.Min.Y + 68
	}
	panelWidth := min(360, rect.Dx()-32)
	x := rect.Min.X + (rect.Dx()-panelWidth)/2
	text = a.fitText(strings.ReplaceAll(text, "\n", " "), panelWidth-24)
	a.label(rect.Min.X+(rect.Dx()-a.textWidth(text))/2, panelTop+24, text, ui.Text)
	if !showProgress {
		return
	}
	bar := image.Rect(x+16, panelTop+40, x+panelWidth-16, panelTop+48)
	a.fill(bar, ui.Field)
	bar.Max.X = bar.Min.X + bar.Dx()*2/3
	a.fill(bar, loadingPreviewColor(progress, ui.Accent))
	phase := i18n.Tr("loading.preview_phase")
	a.label(rect.Min.X+(rect.Dx()-a.textWidth(phase))/2, panelTop+72, phase, ui.Muted)
}
