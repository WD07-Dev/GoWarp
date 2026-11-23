package vm

import (
	"fmt"

	"gowarp/core/extension"
)

func (c *extensionContext) textCall(operation string, args []any) (any, error) {
	t := c.th.target
	a := hostArgs(args)
	switch operation {
	case "graphics.text.get":
		if t.state.Text == nil {
			return map[string]any{}, nil
		}
		s := t.state.Text
		return map[string]any{
			"text":              s.Text,
			"font":              s.Font,
			"color":             s.Color,
			"outlineColor":      s.OutlineColor,
			"width":             s.Width,
			"outlineWidth":      s.OutlineWidth,
			"visible":           s.Visible,
			"fontSize":          s.FontSize,
			"lineHeight":        s.LineHeight,
			"lineHeightDivisor": s.LineHeightDivisor,
			"paddingY":          s.PaddingY,
			"anchorY":           s.AnchorY,
			"align":             s.Align,
			"screenWidth":       s.ScreenWidth,
		}, nil
	case "graphics.text.set":
		values, ok := a.get(0).(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graphics.text.set requires a property map")
		}
		skin := TextSkin{}
		if t.state.Text != nil {
			skin = *t.state.Text
		}
		for key, v := range values {
			switch key {
			case "text":
				skin.Text = extension.String(v)
			case "font":
				skin.Font = extension.String(v)
			case "color":
				skin.Color = extension.String(v)
			case "outlineColor":
				skin.OutlineColor = extension.String(v)
			case "width":
				skin.Width = extension.Number(v)
			case "outlineWidth":
				skin.OutlineWidth = extension.Number(v)
			case "visible":
				skin.Visible = extension.Bool(v)
			case "fontSize":
				skin.FontSize = extension.Number(v)
			case "lineHeight":
				skin.LineHeight = extension.Number(v)
			case "lineHeightDivisor":
				skin.LineHeightDivisor = extension.Number(v)
			case "paddingY":
				skin.PaddingY = extension.Number(v)
			case "anchorY":
				skin.AnchorY = extension.Number(v)
			case "align":
				skin.Align = extension.Number(v)
			case "screenWidth":
				skin.ScreenWidth = extension.Bool(v)
			default:
				return nil, fmt.Errorf("unknown text property %q", key)
			}
		}
		skin = skin.Defaults()
		t.state.Text = &skin
		t.requestRedraw()
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}
