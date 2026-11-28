package extension

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	e "gowarp/core/extension"
)

type filterSpec struct {
	Mode           int
	Amount         float64
	X, Y           float64
	Color1, Color2 []float64
}
type canvasFilter struct {
	name, source string
	spec         filterSpec
}
type filters struct{ entries []canvasFilter }

func New() e.Extension {
	state := &filters{}
	x := e.Extension{ID: "SPspriteEffectsV2", State: map[string]any{"filters": state}}
	add := func(op, kind string, inputs []string, handler e.Handler) {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "SPspriteEffectsV2_" + op, Kind: kind, Inputs: inputs, Handler: handler})
	}
	for _, key := range []string{"EFFECTS", "BLUR_TYPES", "SPLIT_MODES"} {
		add("menu_"+key, "reporter", nil, func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field(key)}, nil })
	}
	add("applySimpleEffect", "reporter", []string{"SRC", "EFFECT", "NUM"}, simpleEffect)
	add("applySpecialBlur", "reporter", []string{"SRC", "TYPE", "x", "y"}, func(c *e.Context) (e.Result, error) {
		if c.String("TYPE") != "motion" {
			return e.Result{}, fmt.Errorf("Sprite Effects V2: unsupported blur %q", c.String("TYPE"))
		}
		x, y := math.Abs(c.Number("x")), math.Abs(c.Number("y"))
		return inject(c, filterSpec{Mode: 0, X: x, Y: y}, fmt.Sprintf(`<feGaussianBlur stdDeviation="%g,%g" in="SourceGraphic"/>`, x, y))
	})
	add("applyColorSplit", "reporter", []string{"SRC", "TYPE", "x", "y", "COL1", "COL2", "COL3"}, func(c *e.Context) (e.Result, error) {
		if c.String("TYPE") != "chroma" {
			return e.Result{}, fmt.Errorf("Sprite Effects V2: unsupported color split %q", c.String("TYPE"))
		}
		color1, err := color(c.String("COL1"))
		if err != nil {
			return e.Result{}, err
		}
		color2, err := color(c.String("COL2"))
		if err != nil {
			return e.Result{}, err
		}
		x, y := c.Number("x"), -c.Number("y")
		body := fmt.Sprintf(`<feOffset in="SourceGraphic" dx="%g" dy="%g" result="one"/><feColorMatrix in="one" result="c1" type="matrix" values="%g 0 0 0 0 0 %g 0 0 0 0 0 %g 0 0 0 0 0 %g 0"/><feOffset in="SourceGraphic" dx="%g" dy="%g" result="two"/><feColorMatrix in="two" result="c2" type="matrix" values="%g 0 0 0 0 0 %g 0 0 0 0 0 %g 0 0 0 0 0 %g 0"/><feBlend in="c1" in2="c2" mode="screen"/>`, x, y, color1[0], color1[1], color1[2], color1[3], -x, -y, color2[0], color2[1], color2[2], color2[3])
		return inject(c, filterSpec{Mode: 3, X: x, Y: y, Color1: color1, Color2: color2}, body)
	})
	add("addCanvasFilter", "command", []string{"NAME", "FILTER"}, func(c *e.Context) (e.Result, error) {
		name, source := c.String("NAME"), c.String("FILTER")
		i := slices.IndexFunc(state.entries, func(f canvasFilter) bool { return f.name == name })
		if i >= 0 && state.entries[i].source == source {
			return e.Result{}, nil
		}
		start := strings.Index(source, "<filter ")
		if start < 0 {
			return e.Result{}, nil
		}
		end := strings.Index(source[start:], "</filter>")
		if end < 0 {
			return e.Result{}, nil
		}
		var tag struct {
			Data string `xml:"data-gowarp,attr"`
		}
		if err := xml.Unmarshal([]byte(source[start:start+end+9]), &tag); err != nil {
			return e.Result{}, err
		}
		data, err := base64.StdEncoding.DecodeString(tag.Data)
		if err != nil {
			return e.Result{}, err
		}
		var spec filterSpec
		if tag.Data == "" {
			return e.Result{}, fmt.Errorf("Sprite Effects V2: custom SVG filters are not supported; use a supported filter reporter")
		}
		if err = json.Unmarshal(data, &spec); err != nil {
			return e.Result{}, err
		}
		entry := canvasFilter{name: name, source: source, spec: spec}
		if i < 0 {
			state.entries = append(state.entries, entry)
		} else {
			state.entries[i] = entry
		}
		return state.apply(c)
	})
	add("removeCanvasFilter", "command", []string{"NAME"}, func(c *e.Context) (e.Result, error) {
		i := slices.IndexFunc(state.entries, func(f canvasFilter) bool { return f.name == c.String("NAME") })
		if i < 0 {
			return e.Result{}, nil
		}
		state.entries = slices.Delete(state.entries, i, i+1)
		return state.apply(c)
	})
	clear := func(c *e.Context) (e.Result, error) {
		if len(state.entries) == 0 {
			return e.Result{}, nil
		}
		state.entries = nil
		return state.apply(c)
	}
	add("removeAllFilters", "command", nil, clear)
	x.Events = map[string]e.Handler{"load": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("graphics.shader.define", "SPspriteEffectsV2.filter", filterShader)
		return e.Result{}, err
	}, "flag": clear, "stop": clear, "cancel": clear}
	return x
}

func (s *filters) apply(c *e.Context) (e.Result, error) {
	width, err := c.Call("camera.get", "width")
	if err != nil {
		return e.Result{}, err
	}
	height, err := c.Call("camera.get", "height")
	if err != nil {
		return e.Result{}, err
	}
	passes := make([]any, 0, len(s.entries))
	for _, entry := range s.entries {
		f := entry.spec
		if f.Mode == 0 && f.X == 0 && f.Y == 0 || (f.Mode == 1 || f.Mode == 2) && f.Amount == 0 {
			continue
		}
		c1, c2 := f.Color1, f.Color2
		if len(c1) != 4 {
			c1 = []float64{1, 1, 1, 1}
		}
		if len(c2) != 4 {
			c2 = []float64{1, 1, 1, 1}
		}
		passes = append(passes, map[string]any{"name": "SPspriteEffectsV2.filter", "uniforms": map[string]any{
			"Mode": f.Mode, "Amount": f.Amount, "Offset": []float64{f.X, f.Y},
			"Color1": c1, "Color2": c2, "StageSize": []float64{e.Number(width), e.Number(height)},
		}})
	}
	_, err = c.Call("graphics.shader.pipeline", passes)
	return e.Result{}, err
}

func simpleEffect(c *e.Context) (e.Result, error) {
	amount := c.Number("NUM")
	switch c.String("EFFECT") {
	case "blur":
		return inject(c, filterSpec{X: math.Abs(amount), Y: math.Abs(amount)}, fmt.Sprintf(`<feGaussianBlur stdDeviation="%g" in="SourceGraphic"/>`, math.Abs(amount)))
	case "contrast":
		k := (amount + 100) / 100
		return inject(c, filterSpec{Mode: 1, Amount: amount}, fmt.Sprintf(`<feComponentTransfer><feFuncR type="gamma" exponent="%g" amplitude="%g"/><feFuncG type="gamma" exponent="%g" amplitude="%g"/><feFuncB type="gamma" exponent="%g" amplitude="%g"/></feComponentTransfer>`, k, k, k, k, k, k))
	case "bloom v2":
		return inject(c, filterSpec{Mode: 2, Amount: amount, X: 5, Y: 5}, fmt.Sprintf(`<feGaussianBlur in="SourceGraphic" stdDeviation="5"/><feComposite in2="SourceGraphic" operator="arithmetic" k2="%g" k3="1" color-interpolation-filters="sRGB"/>`, amount/100))
	default:
		return e.Result{}, fmt.Errorf("Sprite Effects V2: unsupported effect %q", c.String("EFFECT"))
	}
}

func inject(c *e.Context, spec filterSpec, body string) (e.Result, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return e.Result{}, err
	}
	source := c.String("SRC")
	if !strings.HasPrefix(source, "<svg") {
		source = "<svg>CANVAS</svg>"
	}
	start, end := strings.Index(source, ">"), strings.LastIndex(source, "</svg>")
	if start < 0 || end < start {
		return e.Result{Value: source}, nil
	}
	id := "gowarp-filter-" + strconv.Itoa(strings.Count(source, "<filter"))
	filter := `<filter id="` + id + `" data-gowarp="` + base64.StdEncoding.EncodeToString(data) + `" filterUnits="userSpaceOnUse">` + body + `</filter>`
	return e.Result{Value: source[:start+1] + filter + `<g filter="url(#` + id + `)">` + source[start+1:end] + `</g>` + source[end:]}, nil
}

func color(s string) ([]float64, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return nil, fmt.Errorf("Sprite Effects V2: invalid color %q", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return nil, err
	}
	if len(s) == 6 {
		n = n<<8 | 255
	}
	return []float64{float64(n>>24) / 255, float64(n>>16&255) / 255, float64(n>>8&255) / 255, float64(n&255) / 255}, nil
}

const filterShader = `//kage:unit pixels
package main
var Mode, Amount float
var Offset, StageSize vec2
var Color1, Color2 vec4
func linear(c vec4) vec4 {
    if c.a==0 {return c}
    rgb:=c.rgb/c.a
    rgb=mix(rgb/12.92,pow((rgb+0.055)/1.055,vec3(2.4)),step(vec3(0.04045),rgb))
    return vec4(rgb*c.a,c.a)
}
func srgb(c vec4) vec4 {
    if c.a==0{return c}
    rgb:=max(c.rgb/c.a,vec3(0))
    rgb=mix(rgb*12.92,1.055*pow(rgb,vec3(1.0/2.4))-0.055,step(vec3(0.0031308),rgb))
    return vec4(rgb*c.a,c.a)
}
func sample(p vec2) vec4 {
    origin:=imageSrc0Origin()
    q:=p-origin-vec2(0.5)
    f:=fract(q)
    b:=floor(q)+origin+vec2(0.5)
    return mix(mix(linear(imageSrc0At(b)),linear(imageSrc0At(b+vec2(1,0))),f.x),mix(linear(imageSrc0At(b+vec2(0,1))),linear(imageSrc0At(b+vec2(1,1))),f.x),f.y)
}
func Fragment(dst vec4,p vec2,color vec4) vec4 {
    base:=imageSrc0At(p)
    scale:=imageSrc0Size()/max(StageSize,vec2(1))
    if Mode==1 {
        c:=linear(base)
        if c.a==0{return c}
        k:=(Amount+100)/100
        return srgb(vec4(clamp(k*pow(max(c.rgb/c.a,vec3(0.000001)),vec3(k)),vec3(0),vec3(1))*c.a,c.a))
    }
    if Mode==3 {
        a:=sample(p-Offset*scale)*Color1
        b:=sample(p+Offset*scale)*Color2
        a.rgb*=Color1.a
        b.rgb*=Color2.a
        return srgb(vec4(a.rgb+b.rgb-a.rgb*b.rgb,a.a+b.a-a.a*b.a))
    }
    sum:=vec4(0)
    total:=0.0
    for y:=-3;y<=3;y++ {
        if Offset.y==0 && y!=0 {continue}
        for x:=-3;x<=3;x++ {
            if Offset.x==0 && x!=0 {continue}
            weight:=exp(-float(x*x+y*y)*0.5)
            sum+=sample(p+vec2(float(x),float(y))*Offset*scale)*weight
            total+=weight
        }
    }
    blurred:=srgb(sum/total)
    if Mode==2 {return clamp(base+blurred*Amount/100,vec4(0),vec4(1))}
    return blurred
}
`
