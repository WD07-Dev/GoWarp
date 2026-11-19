package extension

import (
	"fmt"
	"reflect"
	"testing"
)

type textHost struct {
	properties map[string]any
}

func (h *textHost) Call(operation string, args ...any) (any, error) {
	switch operation {
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
	got, err := textCall(host, "get", nil)
	if err != nil || !reflect.DeepEqual(got, properties) {
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
