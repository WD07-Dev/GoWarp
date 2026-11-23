package vm

import "testing"

func TestGenericTextLayoutIsIndependentOfExtension(t *testing.T) {
	r := motionRuntime(t, `{}`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	properties := map[string]any{"text": "hello", "fontSize": 40.0, "lineHeight": 100.0, "lineHeightDivisor": 2.0, "paddingY": 3.0, "anchorY": 7.0, "align": 0.0, "screenWidth": false}
	if _, err := c.Call("graphics.text.set", properties); err != nil {
		t.Fatal(err)
	}
	value, err := c.Call("graphics.text.get")
	if err != nil {
		t.Fatal(err)
	}
	got := value.(map[string]any)
	for key, want := range properties {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
}

func TestTextSkinSurvivesCloneAndRestoresCostume(t *testing.T) {
	r := motionRuntime(t, `{
	 "flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"font"},
	 "font":{"opcode":"text_setFont","fields":{"FONT":["Galmuri14",null]},"next":"text"},
	 "text":{"opcode":"text_setText","inputs":{"TEXT":[1,[10,"한글"]]},"next":"clone"},
	 "clone":{"opcode":"control_create_clone_of","inputs":{"CLONE_OPTION":[1,[10,"_myself_"]]}}
	}`)
	run(t, r, 1, false)
	if r.cloneCount != 1 {
		t.Fatal("text prevented clone creation")
	}
	skin := r.targets[1].state.Text
	if skin == nil || skin.Text != "한글" || skin.Font != "Galmuri14" || !skin.Visible {
		t.Fatalf("incorrect text skin: %+v", skin)
	}
	if r.targets[2].state.Text != skin {
		t.Fatal("clone lost its text skin")
	}
}
