package packager

import (
	"image"
	"image/color"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"gowarp/app/i18n"
	"gowarp/app/ui"
)

var sectionHints = []string{
	"hint.project",
	"hint.runtime",
	"hint.window",
	"hint.player",
	"hint.cloud",
	"hint.loading",
	"hint.platform",
	"hint.extensions",
}

func (a *application) fitText(text string, width int) string {
	if a.textWidth(text) <= width {
		return text
	}
	for len(text) > 0 && a.textWidth(text+"…") > width {
		_, n := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-n]
	}
	return text + "…"
}

func (a *application) renderSettings() {
	panel := color.RGBA{39, 39, 39, 255}
	a.fill(image.Rect(16, 78, 176, a.height-128), ui.Field)
	sectionSpacing := min(48, (a.height-260)/(len(sections)-1))
	for i, name := range sections {
		y := 94 + i*sectionSpacing
		r := image.Rect(24, y, 168, y+38)
		if a.section == i {
			a.fill(r, ui.Background)
			a.fill(image.Rect(r.Min.X, y, r.Min.X+3, y+38), ui.Accent)
		}
		a.label(40, y+25, a.fitText(i18n.Tr(name), 120), ui.Text)
		a.hits = append(a.hits, hit{
			rect: r,
			action: func() {
				a.section = i
				a.scroll = 0
				a.editor = nil
				a.keyboardFocus = -1
			},
		})
	}
	left, right := 200, a.width-24
	a.label(left, 100, i18n.Tr(sections[a.section]), ui.Text)
	a.label(left, 126, a.fitText(i18n.Tr(sectionHints[a.section]), right-left), ui.Muted)
	if a.section == 7 {
		a.button(left, 152, 240, i18n.Tr("extensions.open"), a.openExtensionEditor)
		a.label(left, 220, i18n.Tr("extensions.description"), ui.Muted)
		a.label(left, 248, i18n.Tr("extensions.save_hint"), ui.Muted)
		return
	}
	type positionedField struct {
		index int
		rect  image.Rectangle
	}
	var fields []positionedField
	var connection image.Rectangle
	inConnection, customProvider := false, false
	fieldTop := 146
	if a.section == 6 {
		fieldTop = 256
	}
	y, column := fieldTop, 0
	half := (right - left - 12) / 2
	for _, i := range a.form.visibleFields(a.section, a.targetOS()) {
		f := a.form.fields[i]
		connectionField := a.section == 4 && f.kind != "variable" && f.key != "cloud.mode"
		if connectionField && !inConnection {
			y += 36
		} else if !connectionField && inConnection {
			y += 16
		}
		inConnection = connectionField
		if f.key == "cloud.provider" {
			customProvider = f.value == "custom"
		}
		compact := f.kind == "bool" || f.kind == "number" || f.key == "loading.progress" || f.key == "platform.position"
		if (!compact || f.key == "loading.width") && column == 1 {
			y += 96
			column = 0
		}
		x, width := left, right-left
		if connectionField {
			x += 12
			width -= 24
		}
		if compact {
			x += column * (half + 12)
			width = half
		}
		height := 84
		if f.kind == "loadingPreview" {
			height = 204
		}
		fields = append(fields, positionedField{i, image.Rect(x, y, x+width, y+height)})
		if connectionField {
			if connection.Empty() {
				connection = image.Rect(left, y-36, right, y+height+12)
			} else {
				connection.Max.Y = y + height + 12
			}
		}
		if f.key == "platform.fullscreen" && f.value == "true" {
			column = 0
			y += height + 12
		} else if compact && column == 0 {
			column = 1
		} else {
			column = 0
			y += height + 12
		}
	}
	if column == 1 {
		y += 96
	}
	bottom := a.height - 128
	contentHeight := y - fieldTop
	viewportHeight := bottom - fieldTop
	maxScroll := max(0, contentHeight-viewportHeight)
	a.scroll = min(a.scroll, maxScroll)
	canvas := a.pixels
	a.pixels = canvas.SubImage(a.pixelRect(image.Rect(left, fieldTop, right, bottom))).(*image.RGBA)
	if !connection.Empty() {
		r := connection.Sub(image.Pt(0, a.scroll))
		a.fill(r, color.RGBA{75, 75, 75, 255})
		a.fill(r.Inset(1), ui.Background)
		a.label(r.Min.X+12, r.Min.Y+24, i18n.Tr("cloud.connection"), ui.Muted)
	}
	for _, position := range fields {
		f := &a.form.fields[position.index]
		r := position.rect.Sub(image.Pt(0, a.scroll))
		if r.Max.Y <= fieldTop || r.Min.Y >= bottom {
			continue
		}
		a.fill(r, panel)
		label := f.label
		if f.key == "cloud.projectId" && customProvider {
			label = "field.cloud_custom_id"
		}
		if f.kind != "variable" {
			label = i18n.Tr(label)
		}
		a.label(r.Min.X+12, r.Min.Y+24, a.fitText(label, r.Dx()-24), ui.Muted)
		input := image.Rect(r.Min.X+12, r.Min.Y+36, r.Max.X-12, r.Min.Y+70)
		start := len(a.hits)
		if f.kind == "icon" {
			a.renderIcon(r)
		} else if f.kind == "loadingImage" {
			a.renderLoadingImage(r, f.key)
		} else if f.kind == "loadingPreview" {
			a.renderLoadingPreview(image.Rect(r.Min.X+12, r.Min.Y+36, r.Max.X-12, r.Max.Y-12))
		} else if len(f.choices) > 0 {
			selected := 0
			for i, v := range f.choices {
				if v == f.value {
					selected = i
				}
			}
			a.choice(input.Min.X, input.Min.Y, input.Dx(), f.choices, selected, func(i int) {
				previous := a.form.visibleFields(a.section, a.targetOS())
				f.value = f.choices[i]
				if !slices.Equal(previous, a.form.visibleFields(a.section, a.targetOS())) {
					a.scroll = 0
					a.keyboardFocus = -1
				}
			})
		} else {
			a.input(input.Min.X, input.Min.Y, input.Dx(), &f.value)
		}
		// Do not expose clipped fields to keyboard focus or pointer input.
		if input.Min.Y < fieldTop || input.Max.Y > bottom {
			a.hits = a.hits[:start]
		}
	}
	a.pixels = canvas
	if maxScroll > 0 {
		height := max(24, viewportHeight*viewportHeight/contentHeight)
		y := fieldTop + a.scroll*(viewportHeight-height)/maxScroll
		a.fill(image.Rect(right+8, fieldTop, right+11, bottom), ui.Field)
		a.fill(image.Rect(right+8, y, right+11, y+height), ui.Muted)
	}
	if a.section == 0 && a.form.base.Project != "" {
		a.label(left, bottom-12, a.fitText(i18n.Tr("status.selected_project", filepath.Base(a.form.base.Project)), right-left), ui.Muted)
	}
	if a.section == 6 {
		a.fill(image.Rect(left, 146, right, 240), panel)
		a.label(left+12, 172, i18n.Tr("field.platform"), ui.Muted)
		choices := make([]string, len(a.templates))
		for i, t := range a.templates {
			choices[i] = t.OS + " / " + t.Arch
		}
		a.choice(left+12, 184, right-left-24, choices, a.target, func(i int) {
			a.target = i
			a.scroll = 0
			a.editor = nil
			a.keyboardFocus = -1
		})
	}
}
