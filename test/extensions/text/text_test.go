package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"reflect"
	"testing"
)

type textHost struct {
	properties map[string]any
}

func (h *textHost) Call(operation string, args ...any) (any, error) {
	switch operation {
	case "camera.get":
		return 480.0, nil
	case "graphics.text.get":
		return h.properties, nil
	case "graphics.text.set":
		h.properties = args[0].(map[string]any)
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected host operation %s", operation)
}

func TestAnimatedTextSuppliesLayoutWithoutChangingProtocol(t *testing.T) {
	host := &textHost{}
	properties := map[string]any{"text": "hello", "width": 480.0, "visible": true}
	if _, err := textCall(host, "set", []any{properties}); err != nil {
		t.Fatal(err)
	}
	wantLayout := map[string]any{
		"fontSize": 24.0, "lineHeight": 192.0, "lineHeightDivisor": 7.0,
		"paddingY": 3.4285714285714284, "anchorY": 25.02857142857143,
		"align": .5, "screenWidth": true,
	}
	for key, want := range wantLayout {
		if got := host.properties[key]; got != want {
			t.Errorf("layout %s = %v, want %v", key, got, want)
		}
	}
	if len(properties) != 3 {
		t.Fatal("animatedtext.set mutated caller properties")
	}
	want := map[string]any{"text": "hello", "width": 480.0, "visible": true, "align": .5}
	got, err := textCall(host, "get", nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("animatedtext.get exposed native layout: %v, %v", got, err)
	}
}

func TestAnimatedTextRejectsNativeOnlyLayoutProperties(t *testing.T) {
	host := &textHost{}
	if _, err := textCall(host, "set", []any{map[string]any{"fontSize": 40}}); err == nil {
		t.Fatal("native-only property changed the extension protocol")
	}
	if host.properties != nil {
		t.Fatal("invalid update reached the host")
	}
}

func TestWidthAndAlignmentSurviveTextChange(t *testing.T) {
	blocks := map[string]e.Block{}
	for _, b := range New().Blocks {
		blocks[b.Opcode] = b
	}
	for _, tc := range []struct {
		align string
		want  float64
	}{{"left", 0}, {"center", .5}, {"right", 1}, {"unknown", 0}} {
		t.Run(tc.align, func(t *testing.T) {
			h := &textHost{properties: map[string]any{}}
			b, ok := blocks["text_setWidth"]
			if !ok {
				t.Fatal("setWidth is not registered")
			}
			_, err := b.Handler(&e.Context{Host: h, Args: map[string]any{"WIDTH": "200"}, Fields: map[string]string{"ALIGN": tc.align}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = blocks["text_setText"].Handler(&e.Context{Host: h, Args: map[string]any{"TEXT": "hello"}})
			if err != nil {
				t.Fatal(err)
			}
			if h.properties["width"] != 200.0 || h.properties["align"] != tc.want || h.properties["text"] != "hello" {
				t.Fatalf("properties: %#v", h.properties)
			}
		})
	}
}
