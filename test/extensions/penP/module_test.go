package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"reflect"
	"testing"
)

type hostCall struct {
	op   string
	args []any
}
type penHost struct {
	target, storage string
	surfaces        map[string]bool
	calls           []hostCall
}

func (h *penHost) Call(op string, args ...any) (any, error) {
	h.calls = append(h.calls, hostCall{op, args})
	switch op {
	case "graphics.penColor":
		return []float64{1, 1, 1, 1}, nil
	case "target.costumeIndex":
		return float64(2), nil
	case "target.set":
		return nil, nil
	case "target.get":
		if args[0] == "currentCostume" {
			return float64(0), nil
		}
		if args[0] != "id" {
			return nil, fmt.Errorf("unexpected target property %v", args[0])
		}
		return h.target, nil
	case "project.storage":
		return h.storage, nil
	case "graphics.surface.exists":
		return h.surfaces[e.String(args[0])], nil
	case "graphics.surface.create":
		h.surfaces[e.String(args[0])] = true
	case "graphics.surface.delete":
		delete(h.surfaces, e.String(args[0]))
	case "graphics.surface.reset":
		h.surfaces = map[string]bool{}
	case "graphics.surface.clear", "graphics.surface.triangle", "graphics.shader.define", "graphics.stamp", "graphics.shader.uniform":
	default:
		return nil, fmt.Errorf("unexpected operation %s", op)
	}
	return nil, nil
}
func setupPen() (e.Extension, *e.Context, *penHost) {
	x := New()
	h := &penHost{target: "sprite-one", surfaces: map[string]bool{}}
	return x, &e.Context{State: x.State, Host: h}, h
}
func runPen(t *testing.T, x e.Extension, c *e.Context, op string, args map[string]any) any {
	t.Helper()
	c.Args = args
	for _, b := range x.Blocks {
		if b.Opcode == "penP_"+op {
			r, err := b.Handler(c)
			if err != nil {
				t.Fatal(err)
			}
			return r.Value
		}
	}
	t.Fatalf("missing opcode penP_%s", op)
	return nil
}
func lastDraw(t *testing.T, h *penHost) []any {
	t.Helper()
	for i := len(h.calls) - 1; i >= 0; i-- {
		if h.calls[i].op == "graphics.surface.triangle" {
			return h.calls[i].args
		}
	}
	t.Fatal("no triangle submitted")
	return nil
}
func TestTriangleDefaultsAndTargetIsolation(t *testing.T) {
	x, c, h := setupPen()
	runPen(t, x, c, "drawSolidTri", map[string]any{"x1": -9, "y1": 3, "x2": 7, "y2": 11, "x3": 15, "y3": -2})
	want := []any{"Scratch Stage", e.SurfaceTriangle{Vertices: [3]e.TriangleVertex{
		{X: -9, Y: 3, R: 1, G: 1, B: 1, A: 1, Z: -1, W: 1},
		{X: 7, Y: 11, U: 1, V: 1, R: 1, G: 1, B: 1, A: 1, Z: -1, W: 1},
		{X: 15, Y: -2, U: 1, R: 1, G: 1, B: 1, A: 1, Z: -1, W: 1},
	}, MultiplyTextureAlpha: true}}
	if got := lastDraw(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("draw = %#v, want %#v", got, want)
	}
	runPen(t, x, c, "setTrianglePointAttribute", map[string]any{"point": 2, "attribute": 0, "value": 0.25})
	h.target = "clone-two"
	runPen(t, x, c, "drawTexTri", map[string]any{"tex": "costume exact name"})
	if got := lastDraw(t, h); got[1].(e.SurfaceTriangle).Texture != (e.SurfaceTexture{Kind: "costume", Costume: 2}) || got[1].(e.SurfaceTriangle).Vertices[1].U != float64(1) {
		t.Fatalf("clone inherited source attributes: %#v", got)
	}
	h.target = "sprite-one"
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[1].(e.SurfaceTriangle).Vertices[1].U; got != 0.25 {
		t.Fatalf("source UV = %v", got)
	}
	runPen(t, x, c, "resetWholeTriangleAttributes", nil)
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[1].(e.SurfaceTriangle).Vertices[1].U; got != float64(1) {
		t.Fatalf("reset UV = %v", got)
	}
}
func TestTriangleAttributeClampsAndDepth(t *testing.T) {
	x, c, h := setupPen()
	for _, v := range []struct {
		attr  int
		value float64
	}{{2, -12}, {3, 125}, {4, 25}, {5, -17}, {6, 0.5}, {7, 1200}} {
		runPen(t, x, c, "setWholeTrianglePointAttribute", map[string]any{"wholeAttribute": v.attr, "value": v.value})
	}
	runPen(t, x, c, "setTrianglePointAttribute", map[string]any{"point": 3, "attribute": 7, "value": 50})
	runPen(t, x, c, "setTrianglePointAttribute", map[string]any{"point": 0, "attribute": 2, "value": 100})
	runPen(t, x, c, "setWholeTrianglePointAttribute", map[string]any{"wholeAttribute": 19, "value": 50})
	runPen(t, x, c, "drawSolidTri", nil)
	for i, row := range lastDraw(t, h)[1].(e.SurfaceTriangle).Vertices {
		alpha := 10.0
		if i == 2 {
			alpha = 0.5
		}
		want := [6]float64{0, 1, 0.25, alpha, -18, 1}
		if got := [6]float64{row.R, row.G, row.B, row.A, row.Z, row.W}; got != want {
			t.Fatalf("point %d attributes = %#v, want %#v", i, row, want)
		}
	}
}
func TestRenderSurfaceSelectionPrefixAndClear(t *testing.T) {
	x, c, h := setupPen()
	h.storage = `{"prefixes":{"renderTextures":"rt:"}}`
	if _, err := x.Events["load"](c); err != nil {
		t.Fatal(err)
	}
	runPen(t, x, c, "createRenderTextureOfSize", map[string]any{"name": "buffer", "width": 31, "height": 19})
	if got := h.calls[len(h.calls)-1]; got.op != "graphics.surface.create" || !reflect.DeepEqual(got.args, []any{"rt:buffer", float64(31), float64(19)}) {
		t.Fatalf("create = %#v", got)
	}
	runPen(t, x, c, "createRenderTextureOfSize", map[string]any{"name": "Scratch Stage", "width": 1, "height": 1})
	if h.surfaces["rt:Scratch Stage"] {
		t.Fatal("created reserved stage")
	}
	for name, want := range map[string]bool{"Scratch Stage": true, "rt:buffer": true, "buffer": false} {
		if got := runPen(t, x, c, "doesRenderTextureExist", map[string]any{"name": name}); got != want {
			t.Fatalf("exists %s = %v", name, got)
		}
	}
	runPen(t, x, c, "clearRenderTexture", map[string]any{"name": "rt:buffer"})
	runPen(t, x, c, "clearRenderTexture", map[string]any{"name": "missing"})
	runPen(t, x, c, "clearRenderTexture", map[string]any{"name": "Scratch Stage"})
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[0]; got != "rt:buffer" {
		t.Fatalf("clear selection = %v", got)
	}
	for _, call := range h.calls {
		if call.op == "graphics.surface.clear" && call.args[0] != "rt:buffer" {
			t.Fatalf("cleared invalid surface: %#v", call)
		}
	}
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": "missing"})
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[0]; got != "Scratch Stage" {
		t.Fatalf("invalid selection = %v", got)
	}
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": "rt:buffer"})
	runPen(t, x, c, "drawTexTri", map[string]any{"tex": "rt:buffer"})
	if got := lastDraw(t, h); got[0] != "rt:buffer" || got[1].(e.SurfaceTriangle).Texture != (e.SurfaceTexture{Kind: "costume", Costume: 2}) {
		t.Fatalf("texture routing = %#v", got)
	}
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": "Scratch Stage"})
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[0]; got != "Scratch Stage" {
		t.Fatalf("stage selection = %v", got)
	}
}
func TestLoadResetsResourcesAndPreservesShaders(t *testing.T) {
	x, c, h := setupPen()
	runPen(t, x, c, "createRenderTextureOfSize", map[string]any{"name": "old", "width": 2, "height": 2})
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": "old"})
	runPen(t, x, c, "setWholeTrianglePointAttribute", map[string]any{"wholeAttribute": 2, "value": 0})
	h.storage = `{"shaders":{"shader":{"projectData":{"fragShader":"fragment","projectData":{"savedVarState":{"v":4}}}}}}`
	if _, err := x.Events["load"](c); err != nil {
		t.Fatal(err)
	}
	if len(h.surfaces) != 0 {
		t.Fatal("load retained surfaces")
	}
	got := h.calls[len(h.calls)-1]
	if got.op != "graphics.shader.define" || !reflect.DeepEqual(got.args, []any{"shader", e.ShaderSource{Code: "fragment", Compiler: ShaderCompiler{}}, map[string]any{"v": float64(4)}}) {
		t.Fatalf("shader = %#v", got)
	}
	runPen(t, x, c, "drawSolidTri", nil)
	draw := lastDraw(t, h)
	if draw[0] != "Scratch Stage" || draw[1].(e.SurfaceTriangle).Vertices[0].R != float64(1) {
		t.Fatalf("load retained state: %#v", draw)
	}
	if x.Events["flag"] != nil {
		t.Fatal("flag must preserve Pen+ state")
	}
}
func TestPenMenusAndExistingHandlers(t *testing.T) {
	x, c, h := setupPen()
	for _, key := range []string{"costumeMenu", "pointMenu", "renderTextures", "renderTexturesOnly", "triAttribute", "wholeTriAttribute", "penPlusShaders", "spriteMenu", "filterType"} {
		c.Fields = map[string]string{key: "chosen"}
		if got := runPen(t, x, c, "menu_"+key, nil); got != "chosen" {
			t.Fatalf("menu %s = %v", key, got)
		}
	}
	runPen(t, x, c, "stampSprite", map[string]any{"sprite": "cat"})
	runPen(t, x, c, "setNumberInShader", map[string]any{"shader": "s", "uniformName": "u", "number": 3})
	runPen(t, x, c, "setVec4InShader", map[string]any{"shader": "s", "uniformName": "v", "numberX": 1, "numberY": 2, "numberZ": 3, "numberW": 4})
	want := []hostCall{{"graphics.stamp", []any{"cat"}}, {"graphics.shader.uniform", []any{"s", "u", float64(3)}}, {"graphics.shader.uniform", []any{"s", "v", []any{float64(1), float64(2), float64(3), float64(4)}}}}
	if !reflect.DeepEqual(h.calls, want) {
		t.Fatalf("existing handlers = %#v", h.calls)
	}
}
func TestFilterAndRemovingSelectedSurface(t *testing.T) {
	x, c, h := setupPen()
	runPen(t, x, c, "createRenderTextureOfSize", map[string]any{"name": "temporary", "width": 7, "height": 5})
	if !h.surfaces["temporary"] {
		t.Fatal("unconfigured prefix must be empty")
	}
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": "temporary"})
	runPen(t, x, c, "removeRenderTexture", map[string]any{"name": "temporary"})
	runPen(t, x, c, "setTriangleFilterMode", map[string]any{"filter": "9729"})
	runPen(t, x, c, "drawTexTri", map[string]any{"tex": "costume"})
	if got := lastDraw(t, h); got[0] != "Scratch Stage" || !got[1].(e.SurfaceTriangle).Linear {
		t.Fatalf("delete/filter = %#v", got)
	}
	if runPen(t, x, c, "doesRenderTextureExist", map[string]any{"name": "temporary"}) != false {
		t.Fatal("removed surface still exists")
	}
	runPen(t, x, c, "setTriangleFilterMode", map[string]any{"filter": "9728"})
	runPen(t, x, c, "drawTexTri", map[string]any{"tex": "costume"})
	if lastDraw(t, h)[1].(e.SurfaceTriangle).Linear {
		t.Fatal("nearest filter not restored")
	}
}

func TestCloneDeletionReleasesOnlyItsTriangleAttributes(t *testing.T) {
	x, c, h := setupPen()
	runPen(t, x, c, "setWholeTrianglePointAttribute", map[string]any{"wholeAttribute": 2, "value": 25})
	h.target = "deleted-clone"
	runPen(t, x, c, "setWholeTrianglePointAttribute", map[string]any{"wholeAttribute": 2, "value": 50})
	handler := x.Events["clone-deleted"]
	if handler == nil {
		t.Fatal("missing clone-deleted cleanup")
	}
	if _, err := handler(c); err != nil {
		t.Fatal(err)
	}
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[1].(e.SurfaceTriangle).Vertices[0].R; got != float64(1) {
		t.Fatalf("deleted clone retained attributes: %v", got)
	}
	h.target = "sprite-one"
	runPen(t, x, c, "drawSolidTri", nil)
	if got := lastDraw(t, h)[1].(e.SurfaceTriangle).Vertices[0].R; got != 0.25 {
		t.Fatalf("deletion changed original attributes: %v", got)
	}
}

func TestEmptyTextureNameIsNotASolidTriangle(t *testing.T) {
	x, c, h := setupPen()
	runPen(t, x, c, "createRenderTextureOfSize", map[string]any{"name": "", "width": 4, "height": 3})
	if !h.surfaces[""] {
		t.Fatal("empty render texture name was changed")
	}
	runPen(t, x, c, "targetRenderTexture", map[string]any{"name": ""})
	runPen(t, x, c, "drawTexTri", map[string]any{"tex": ""})
	if got := lastDraw(t, h); got[0] != "" || got[1].(e.SurfaceTriangle).Texture != (e.SurfaceTexture{Kind: "costume", Costume: 2}) {
		t.Fatalf("empty names = %#v", got)
	}
	runPen(t, x, c, "drawSolidTri", nil)
	if lastDraw(t, h)[1].(e.SurfaceTriangle).Texture.Kind != "" {
		t.Fatal("solid triangle supplied a texture")
	}
}
