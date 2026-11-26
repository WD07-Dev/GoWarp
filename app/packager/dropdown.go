package packager

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"gowarp/app/i18n"
	"gowarp/app/ui"
)

type dropdown struct {
	rect                    image.Rectangle
	choices                 []string
	selected, previousFocus int
	choose                  func(int)
}

func (a *application) choice(x, y, width int, choices []string, selected int, choose func(int)) {
	rect := image.Rect(x, y, x+width, y+34)
	background, foreground := choiceColors(choices, selected)
	a.fill(rect, background)
	label := i18n.Tr("choice.empty")
	if len(choices) > 0 {
		label = displayChoice(choices[selected])
	}
	a.label(x+10, y+23, a.fitText(label, width-44), foreground)
	for row := 0; row < 4; row++ {
		a.fill(image.Rect(x+width-24+row, y+15+row, x+width-17-row, y+16+row), ui.Muted)
	}
	a.hits = append(a.hits, hit{
		rect: rect,
		action: func() {
			if len(choices) == 0 {
				return
			}
			a.dropdown = &dropdown{
				rect:          rect,
				choices:       choices,
				selected:      selected,
				previousFocus: a.keyboardFocus,
				choose:        choose,
			}
			a.editor = nil
			a.keyboardFocus = selected
			a.dirty = true
		},
	})
}

func (a *application) closeDropdown() {
	if a.dropdown == nil {
		return
	}
	a.keyboardFocus = a.dropdown.previousFocus
	a.dropdown = nil
	a.dirty = true
}

func (a *application) selectDropdown(index int) {
	d := a.dropdown
	if d == nil || index < 0 || index >= len(d.choices) {
		return
	}
	a.closeDropdown()
	d.choose(index)
}

func (a *application) dropdownRect() image.Rectangle {
	d := a.dropdown
	height := len(d.choices) * 34
	y := d.rect.Max.Y + 4
	if y+height > a.height-12 {
		y = max(12, d.rect.Min.Y-height-4)
	}
	return image.Rect(d.rect.Min.X, y, d.rect.Max.X, y+height)
}

func (a *application) dropdownClick(x, y int) {
	r := a.dropdownRect()
	if image.Pt(x, y).In(r) {
		a.selectDropdown((y - r.Min.Y) / 34)
	} else {
		a.closeDropdown()
	}
}

func (a *application) updateDropdown() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		a.closeDropdown()
		return
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		x, y = a.logicalPoint(x, y)
		a.dropdownClick(x, y)
		return
	}
	d := a.dropdown
	_, wheel := ebiten.Wheel()
	delta := 0
	if repeated(ebiten.KeyArrowDown) || wheel < 0 {
		delta = 1
	}
	if repeated(ebiten.KeyArrowUp) || wheel > 0 {
		delta = -1
	}
	if delta != 0 {
		d.selected = (d.selected + delta + len(d.choices)) % len(d.choices)
		a.keyboardFocus = d.selected
		a.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		a.selectDropdown(d.selected)
	}
}

func (a *application) renderDropdown() {
	d := a.dropdown
	r := a.dropdownRect()
	a.fill(r.Inset(-2), ui.Muted)
	a.fill(r, ui.Field)
	a.hits = nil
	for i, value := range d.choices {
		rect := image.Rect(r.Min.X, r.Min.Y+i*34, r.Max.X, r.Min.Y+(i+1)*34)
		if i == d.selected {
			a.fill(rect, ui.Background)
			a.fill(image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+3, rect.Max.Y), ui.Accent)
		}
		_, foreground := choiceColors(d.choices, i)
		a.label(rect.Min.X+12, rect.Min.Y+23, a.fitText(displayChoice(value), rect.Dx()-24), foreground)
		a.hits = append(a.hits, hit{
			rect: rect,
			action: func() {
				a.selectDropdown(i)
			},
		})
	}
}

func choiceColors(choices []string, selected int) (color.Color, color.Color) {
	if len(choices) == 2 && choices[0] == "true" && choices[1] == "false" {
		if selected == 0 {
			return color.RGBA{35, 66, 49, 255}, color.RGBA{133, 225, 163, 255}
		}
		return color.RGBA{66, 40, 43, 255}, color.RGBA{238, 163, 168, 255}
	}
	return ui.Field, ui.Text
}
