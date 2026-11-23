package packager

import (
	"context"
	"image"
	"path/filepath"

	"golang.org/x/image/draw"

	"gowarp/app/assets"
	"gowarp/app/i18n"
	"gowarp/app/ui"
)

func (a *application) loadIcon(path string) {
	a.job(i18n.Tr("job.icon"), func(ctx context.Context) result {
		icon, err := assets.LoadIcon(path)
		if err != nil {
			return result{err: err}
		}
		if err := ctx.Err(); err != nil {
			return result{err: err}
		}
		return result{icon: icon, status: i18n.Tr("status.icon_loaded", filepath.Base(path))}
	})
}

func (a *application) renderIcon(rect image.Rectangle) {
	x, y := rect.Min.X+12, rect.Min.Y+36
	a.button(x, y, 116, i18n.Tr("action.choose_image"), func() {
		a.choose("icon")
	})
	if a.form.icon == nil {
		a.label(x+132, y+23, i18n.Tr("icon.default"), ui.Muted)
		return
	}
	a.button(x+128, y, 116, i18n.Tr("icon.reset"), func() {
		a.form.base.Player.Icon = nil
		a.form.icon = nil
	})
	preview := a.pixelRect(image.Rect(rect.Max.X-72, rect.Min.Y+14, rect.Max.X-16, rect.Min.Y+70))
	ui.Fill(a.pixels, preview, ui.Field)
	draw.CatmullRom.Scale(a.pixels, preview, a.form.icon, a.form.icon.Bounds(), draw.Over, nil)
}
