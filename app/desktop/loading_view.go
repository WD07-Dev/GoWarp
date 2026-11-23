package desktop

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font"

	"gowarp/app/assets"
	"gowarp/app/config"
	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/media"
	"gowarp/core/vm"
)

type loadingView struct {
	progress         media.LoadProgress
	images           loadingImages
	background, logo *ebiten.Image
}

func (g *game) drawLoading(screen *ebiten.Image) {
	s := config.DefaultLoadingSettings()
	if settings := g.options.Settings; settings != nil {
		if settings.Loading.Background != "" {
			s.Background = settings.Loading.Background
		}
		if settings.Loading.Progress != "" {
			s.Progress = settings.Loading.Progress
		}
		s.Text = settings.Loading.Text
		s.ShowProgress = settings.Loading.ShowProgress
	}
	background := vm.ParseColor(s.Background)
	screen.Fill(background)
	view := &g.loading
	now := time.Now()
	if err := updateLoadingTexture(view.images.background, &view.background, now); err != nil {
		g.failure = err
	}
	if err := updateLoadingTexture(view.images.logo, &view.logo, now); err != nil {
		g.failure = err
	}
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	face := g.face
	if g.options.Settings != nil {
		face = g.scaledFace(g.dpi(), false)
	}
	if view.background != nil {
		drawLoadingImage(screen, view.background, image.Rect(0, 0, w, h), true)
	}
	if view.logo != nil {
		size := min(g.pixels(160), min(w/2, h/3))
		centerY := h/2 - g.pixels(72)
		drawLoadingImage(screen, view.logo, image.Rect((w-size)/2, centerY-size/2, (w+size)/2, centerY+size/2), false)
	}
	panelWidth := max(1, min(g.pixels(440), w-g.pixels(32)))
	panelHeight := g.pixels(112)
	message := s.Text
	if g.failure != nil {
		message = i18n.Tr("player.error", g.failure.Error())
	}
	lines := wrapText(face, message, max(1, panelWidth-g.pixels(32)))
	panelHeight = max(panelHeight, g.pixels(64+20*len(lines)))
	frame := image.NewRGBA(image.Rect(0, 0, panelWidth, panelHeight))
	for i, line := range lines {
		x := max(g.pixels(8), (panelWidth-font.MeasureString(face, line).Ceil())/2)
		ui.Label(frame, face, x, g.pixels(28+i*20), line, ui.Text)
	}
	if s.ShowProgress && g.failure == nil {
		progress := view.progress
		fraction := 0.0
		if progress.Total > 0 {
			fraction = math.Max(0, math.Min(1, float64(progress.Done)/float64(progress.Total)))
		}
		bar := image.Rect(g.pixels(16), panelHeight-g.pixels(48), panelWidth-g.pixels(16), panelHeight-g.pixels(40))
		ui.Fill(frame, bar, ui.Field)
		bar.Max.X = bar.Min.X + int(float64(bar.Dx())*fraction)
		ui.Fill(frame, bar, vm.ParseColor(s.Progress))
		label := loadingProgressText(progress.Label)
		if label == "" {
			label = i18n.Tr("loading.open_project")
		}
		label = fmt.Sprintf("%s · %d%%", label, int(fraction*100))
		x := max(g.pixels(8), (panelWidth-font.MeasureString(face, label).Ceil())/2)
		ui.Label(frame, face, x, panelHeight-g.pixels(16), label, ui.Text)
	}
	if g.canvas == nil || g.canvas.Bounds() != frame.Bounds() {
		if g.canvas != nil {
			g.canvas.Deallocate()
		}
		g.canvas = ebiten.NewImage(panelWidth, panelHeight)
	}
	g.canvas.WritePixels(frame.Pix)
	op := &ebiten.DrawImageOptions{}
	y := (h - panelHeight) / 2
	if view.logo != nil {
		y = min(h-panelHeight, h/2+g.pixels(16))
	}
	op.GeoM.Translate(float64((w-panelWidth)/2), float64(max(0, y)))
	screen.DrawImage(g.canvas, op)
}

func updateLoadingTexture(picture *assets.LoadingImage, texture **ebiten.Image, now time.Time) error {
	if picture == nil {
		return nil
	}
	changed, err := picture.Advance(now)
	if err != nil {
		return err
	}
	if *texture == nil {
		*texture = ebiten.NewImageFromImage(picture)
	} else if changed {
		(*texture).WritePixels(picture.Image.(*image.RGBA).Pix)
	}
	return nil
}

func loadingProgressText(label string) string {
	switch label {
	case "Opening project":
		return i18n.Tr("loading.open_project")
	case "Preparing to start":
		return i18n.Tr("loading.ready")
	case "Preparing project":
		return i18n.Tr("loading.prepare_project")
	case "Reading costume information":
		return i18n.Tr("loading.costumes")
	case "Reading sound information":
		return i18n.Tr("loading.sounds")
	case "Preparing the first frame":
		return i18n.Tr("loading.first_frame")
	case "Preparing sound effects":
		return i18n.Tr("loading.sound_effects")
	}
	return label
}

func drawLoadingImage(screen, src *ebiten.Image, bounds image.Rectangle, cover bool) {
	if bounds.Empty() {
		return
	}
	sx := float64(bounds.Dx()) / float64(src.Bounds().Dx())
	sy := float64(bounds.Dy()) / float64(src.Bounds().Dy())
	scale := math.Min(sx, sy)
	if cover {
		scale = math.Max(sx, sy)
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(float64(bounds.Min.X)+(float64(bounds.Dx())-float64(src.Bounds().Dx())*scale)/2,
		float64(bounds.Min.Y)+(float64(bounds.Dy())-float64(src.Bounds().Dy())*scale)/2)
	screen.DrawImage(src, op)
}

func (g *game) releaseLoadingImages() {
	if g.loading.background != nil {
		g.loading.background.Deallocate()
	}
	if g.loading.logo != nil {
		g.loading.logo.Deallocate()
	}
	g.loading = loadingView{}
}
