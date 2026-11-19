// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	"fmt"
	e "gowarp/core/extension"
	"math"
	"regexp"
	"sort"
	"strings"
)

func call(c *e.Context, op string, args ...any) (e.Result, error) {
	v, err := c.Call(op, args...)
	return e.Result{Value: v}, err
}
func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "command", Inputs: inputs, Handler: fn, BorrowContext: true}
}
func rep(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: op, Kind: "reporter", Inputs: inputs, Handler: fn, BorrowContext: true}
}
func menu(op, key string) e.Block {
	return rep(op, nil, func(c *e.Context) (e.Result, error) {
		return e.Result{Value: c.Field(key)}, nil
	})
}
func New() e.Extension {
	x := e.Extension{ID: "penP", State: map[string]any{}, Events: map[string]e.Handler{"load": load, "clone-deleted": func(c *e.Context) (e.Result, error) {
		id, err := c.Call("target.get", "id")
		if err != nil {
			return e.Result{}, err
		}
		if triangles, ok := c.State["triangles"].(map[string]*triangleAttributes); ok {
			delete(triangles, e.String(id))
		}
		return e.Result{}, nil
	}}, Blocks: []e.Block{
		menu("penP_menu_penPlusShaders", "penPlusShaders"), menu("penP_menu_spriteMenu", "spriteMenu"),
		cmd("penP_stampSprite", []string{"sprite"}, func(c *e.Context) (e.Result, error) {
			return call(c, "graphics.stamp", c.String("sprite"))
		}),
		cmd("penP_setNumberInShader", []string{"shader", "uniformName", "number"}, func(c *e.Context) (e.Result, error) {
			return call(c, "graphics.shader.uniform", c.String("shader"), c.String("uniformName"), c.Number("number"))
		}),
		cmd("penP_setVec4InShader", []string{"shader", "uniformName", "numberX", "numberY", "numberZ", "numberW"}, func(c *e.Context) (e.Result, error) {
			return call(c, "graphics.shader.uniform", c.String("shader"), c.String("uniformName"), []any{c.Number("numberX"), c.Number("numberY"), c.Number("numberZ"), c.Number("numberW")})
		})}}
	for _, key := range []string{"costumeMenu", "pointMenu", "renderTextures", "renderTexturesOnly", "triAttribute", "wholeTriAttribute", "filterType"} {
		x.Blocks = append(x.Blocks, menu("penP_menu_"+key, key))
	}
	x.Blocks = append(x.Blocks,
		cmd("penP_createRenderTextureOfSize", []string{"name", "width", "height"}, func(c *e.Context) (e.Result, error) {
			if c.String("name") == stageSurface {
				return e.Result{}, nil
			}
			prefix, _ := c.State["renderTexturePrefix"].(string)
			return call(c, "graphics.surface.create", prefix+c.String("name"), c.Number("width"), c.Number("height"))
		}),
		e.Block{Opcode: "penP_doesRenderTextureExist", Kind: "boolean", Inputs: []string{"name"}, BorrowContext: true, Handler: func(c *e.Context) (e.Result, error) {
			if c.String("name") == stageSurface {
				return e.Result{Value: true}, nil
			}
			return call(c, "graphics.surface.exists", c.String("name"))
		}},
		cmd("penP_targetRenderTexture", []string{"name"}, func(c *e.Context) (e.Result, error) {
			name := c.String("name")
			if name != stageSurface {
				exists, err := c.Call("graphics.surface.exists", name)
				if err != nil {
					return e.Result{}, err
				}
				if !e.Bool(exists) {
					name = stageSurface
				}
			}
			c.State["surface"] = name
			return e.Result{}, nil
		}),
		cmd("penP_clearRenderTexture", []string{"name"}, func(c *e.Context) (e.Result, error) {
			name := c.String("name")
			if name == stageSurface {
				return e.Result{}, nil
			}
			exists, err := c.Call("graphics.surface.exists", name)
			if err != nil || !e.Bool(exists) {
				return e.Result{}, err
			}
			c.State["surface"] = name
			return call(c, "graphics.surface.clear", name)
		}),
		cmd("penP_removeRenderTexture", []string{"name"}, func(c *e.Context) (e.Result, error) {
			name := c.String("name")
			if name == stageSurface {
				return e.Result{}, nil
			}
			if _, err := c.Call("graphics.surface.delete", name); err != nil {
				return e.Result{}, err
			}
			if selectedSurface(c) == name {
				c.State["surface"] = stageSurface
			}
			return e.Result{}, nil
		}),
		cmd("penP_setTriangleFilterMode", []string{"filter"}, func(c *e.Context) (e.Result, error) {
			c.State["linear"] = c.Number("filter") == 9729
			return e.Result{}, nil
		}),
		cmd("penP_setTrianglePointAttribute", []string{"point", "attribute", "value"}, func(c *e.Context) (e.Result, error) {
			return setTriangleAttribute(c, false)
		}),
		cmd("penP_setWholeTrianglePointAttribute", []string{"wholeAttribute", "value"}, func(c *e.Context) (e.Result, error) {
			return setTriangleAttribute(c, true)
		}),
		cmd("penP_resetWholeTriangleAttributes", nil, func(c *e.Context) (e.Result, error) {
			attributes, err := targetTriangle(c)
			if err != nil {
				return e.Result{}, err
			}
			*attributes = defaultTriangle()
			return e.Result{}, nil
		}),
		cmd("penP_drawSolidTri", []string{"x1", "y1", "x2", "y2", "x3", "y3"}, func(c *e.Context) (e.Result, error) {
			return drawTriangle(c, nil)
		}),
		cmd("penP_drawTexTri", []string{"x1", "y1", "x2", "y2", "x3", "y3", "tex"}, func(c *e.Context) (e.Result, error) {
			return drawTriangle(c, c.String("tex"))
		}),
	)
	return x
}

const stageSurface = "Scratch Stage"

// Each point stores U, V, R, G, B, Z, W, opacity, matching Pen+ attributes.
type triangleAttributes [3][8]float64

func defaultTriangle() triangleAttributes {
	return triangleAttributes{{0, 0, 1, 1, 1, 0, 1, 1}, {1, 1, 1, 1, 1, 0, 1, 1}, {1, 0, 1, 1, 1, 0, 1, 1}}
}
func targetTriangle(c *e.Context) (*triangleAttributes, error) {
	id, err := c.Call("target.get", "id")
	if err != nil {
		return nil, err
	}
	triangles, ok := c.State["triangles"].(map[string]*triangleAttributes)
	if !ok {
		triangles = map[string]*triangleAttributes{}
		c.State["triangles"] = triangles
	}
	key := e.String(id)
	attributes := triangles[key]
	if attributes == nil {
		defaults := defaultTriangle()
		attributes = &defaults
		triangles[key] = attributes
	}
	return attributes, nil
}
func setTriangleAttribute(c *e.Context, whole bool) (e.Result, error) {
	key := "attribute"
	if whole {
		key = "wholeAttribute"
	}
	attribute := c.Number(key)
	if attribute < 0 || attribute > 7 || math.Trunc(attribute) != attribute {
		return e.Result{}, nil
	}
	point := c.Number("point") - 1
	if !whole && (point < 0 || point > 2 || math.Trunc(point) != point) {
		return e.Result{}, nil
	}
	attributes, err := targetTriangle(c)
	if err != nil {
		return e.Result{}, err
	}
	value := c.Number("value")
	switch int(attribute) {
	case 2, 3, 4:
		value = math.Min(math.Max(value, 0), 100) * 0.01
	case 6:
		value = math.Max(value, 1)
	case 7:
		value = math.Min(math.Max(value, 0), 1000) * 0.01
	}
	if whole {
		for i := range attributes {
			attributes[i][int(attribute)] = value
		}
	} else {
		attributes[int(point)][int(attribute)] = value
	}
	return e.Result{}, nil
}
func selectedSurface(c *e.Context) string {
	name, ok := c.State["surface"].(string)
	if !ok {
		return stageSurface
	}
	return name
}
func drawTriangle(c *e.Context, texture any) (e.Result, error) {
	attributes, err := targetTriangle(c)
	if err != nil {
		return e.Result{}, err
	}
	var vertices [3]e.TriangleVertex
	for i, xy := range [3][2]string{{"x1", "y1"}, {"x2", "y2"}, {"x3", "y3"}} {
		a := attributes[i]
		vertices[i] = e.TriangleVertex{X: c.Number(xy[0]), Y: c.Number(xy[1]), U: a[0], V: a[1], R: a[2], G: a[3], B: a[4], A: a[7], Z: a[5], W: a[6]}
	}
	value, err := drawPenTriangle(c, selectedSurface(c), texture, vertices, e.Bool(c.State["linear"]))
	return e.Result{Value: value}, err
}
func load(c *e.Context) (e.Result, error) {
	if _, err := c.Call("graphics.surface.reset"); err != nil {
		return e.Result{}, err
	}
	c.State["triangles"] = map[string]*triangleAttributes{}
	c.State["surface"] = stageSurface
	c.State["renderTexturePrefix"] = ""
	c.State["linear"] = false
	raw, err := c.Call("project.storage", "penP")
	if err != nil {
		return e.Result{}, err
	}
	if e.String(raw) == "" {
		return e.Result{}, nil
	}
	var data struct {
		Prefixes struct {
			RenderTextures string `json:"renderTextures"`
		} `json:"prefixes"`
		Shaders map[string]struct {
			ProjectData struct {
				Fragment    string `json:"fragShader"`
				ProjectData struct {
					Uniforms map[string]any `json:"savedVarState"`
				} `json:"projectData"`
			} `json:"projectData"`
		} `json:"shaders"`
	}
	if err := json.Unmarshal([]byte(e.String(raw)), &data); err != nil {
		return e.Result{}, err
	}
	c.State["renderTexturePrefix"] = data.Prefixes.RenderTextures
	for name, s := range data.Shaders {
		if _, err := c.Call("graphics.shader.define", name, e.ShaderSource{Code: s.ProjectData.Fragment, Compiler: ShaderCompiler{}}, s.ProjectData.ProjectData.Uniforms); err != nil {
			return e.Result{}, err
		}
	}
	return e.Result{}, nil
}

func drawPenTriangle(host e.Host, destination string, source any, vertices [3]e.TriangleVertex, linear bool) (any, error) {
	if destination != stageSurface {
		exists, err := host.Call("graphics.surface.exists", destination)
		if err != nil || !e.Bool(exists) {
			return nil, err
		}
	}
	for i := range vertices {
		vertex := &vertices[i]
		fields := [...]*float64{&vertex.X, &vertex.Y, &vertex.U, &vertex.V,
			&vertex.R, &vertex.G, &vertex.B, &vertex.A, &vertex.Z, &vertex.W}
		for _, field := range fields {
			if math.IsInf(*field, 0) {
				return nil, nil
			}
			if math.IsNaN(*field) {
				*field = 0
			}
		}
		if vertex.W <= 0 {
			return nil, nil
		}
		vertex.Z--
	}
	var texture e.SurfaceTexture
	if source == nil {
		value, err := host.Call("graphics.penColor")
		if err != nil {
			return nil, err
		}
		rgba, ok := value.([]float64)
		if !ok || len(rgba) != 4 {
			return nil, fmt.Errorf("graphics.penColor did not return RGBA")
		}
		for i := range vertices {
			vertex := &vertices[i]
			vertex.R *= rgba[0]
			vertex.G *= rgba[1]
			vertex.B *= rgba[2]
			vertex.A *= rgba[3]
		}
	} else {
		name := e.String(source)
		exists, err := host.Call("graphics.surface.exists", name)
		if err != nil {
			return nil, err
		}
		if name != stageSurface && name != destination && e.Bool(exists) {
			texture = e.SurfaceTexture{Kind: "surface", Surface: name}
		} else {
			index, err := host.Call("target.costumeIndex", name)
			if err != nil {
				return nil, err
			}
			if e.Number(index) < 0 {
				return nil, nil
			}
			current, err := host.Call("target.get", "currentCostume")
			if err != nil {
				return nil, err
			}
			if e.Number(current) != e.Number(index) {
				if _, err := host.Call("target.set", "currentCostume", index); err != nil {
					return nil, err
				}
			}
			texture = e.SurfaceTexture{Kind: "costume", Costume: e.Number(index)}
		}
	}
	return host.Call("graphics.surface.triangle", destination, e.SurfaceTriangle{
		Vertices: vertices,
		Texture:  texture,
		Linear:   linear,
		// Pen+ multiplies premultiplied texture RGB by alpha a second time.
		MultiplyTextureAlpha: true,
	})
}

// ShaderCompiler translates Pen+ fragment programs and binds their uniforms.
type ShaderCompiler struct{}

func (ShaderCompiler) ID() string {
	return "penplus.glsl"
}

func (ShaderCompiler) Compile(source string) (e.ShaderProgram, error) {
	code, uniforms, err := translateStageShader(source)
	if err != nil {
		return e.ShaderProgram{}, err
	}
	type uniform struct {
		name, key string
		fallback  any
	}
	bindings := make([]uniform, 0, len(uniforms))
	for name, typ := range uniforms {
		var fallback any = float32(0)
		switch typ {
		case "vec2":
			fallback = []float32{0, 0}
		case "vec3":
			fallback = []float32{0, 0, 0}
		case "vec4":
			fallback = []float32{0, 0, 0, 0}
		case "mat4":
			// Preserve Pen+'s existing unset-matrix values.
			fallback = []float32{1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		}
		bindings = append(bindings, uniform{name: name, key: "U_" + name, fallback: fallback})
	}
	return e.ShaderProgram{
		Code: code,
		NewUniforms: func() e.ShaderUniformBinding {
			resolution := make([]float32, 2)
			time := make([]float32, 1)
			elapsed := make([]float32, 1)
			u := make(map[string]any, len(bindings)+4)
			u["StageResolution"] = resolution
			u["StageTime"] = time
			u["StageElapsed"] = elapsed
			defaults := make([]any, len(bindings))
			for i, binding := range bindings {
				defaults[i] = binding.fallback
				if vector, ok := binding.fallback.([]float32); ok {
					defaults[i] = append([]float32(nil), vector...)
				}
			}
			return func(values map[string]any, context e.ShaderContext) map[string]any {
				resolution[0], resolution[1] = float32(context.Width), float32(context.Height)
				time[0], elapsed[0] = float32(context.Time), float32(context.Elapsed)
				nearest := 0
				if context.Nearest {
					nearest = 1
				}
				u["StageNearest"] = nearest
				for i, binding := range bindings {
					value, ok := values[binding.name]
					if !ok {
						value = defaults[i]
						if vector, ok := value.([]float32); ok {
							copy(vector, binding.fallback.([]float32))
						}
					}
					u[binding.key] = value
				}
				return u
			}
		},
	}, nil
}

var shaderComments = regexp.MustCompile(`(?s)/\*.*?\*[/]|//[^\n]*|(?m)^#[^\n]*`)
var shaderFunction = regexp.MustCompile(`\b(?:highp\s+|mediump\s+|lowp\s+)?(void|float|vec[234]|int|bool)\s+(\w+)\s*\(([^()]*)\)\s*\{`)
var shaderUniform = regexp.MustCompile(`\buniform\s+(?:(?:highp|mediump|lowp)\s+)?(float|vec[234]|mat[234]|int|bool|sampler2D)\s+(\w+)\s*;`)
var shaderPrecision = regexp.MustCompile(`\b(?:highp|mediump|lowp)\s+`)
var shaderDeclaration = regexp.MustCompile(`\b(float|vec[234]|int|bool)\s+(\w+)(?:\s*\[\s*(\d+)\s*\])?`)
var shaderConstant = regexp.MustCompile(`\bconst\s+(?:float|int)\s+(\w+)\s*=\s*([^;]+);`)
var shaderFor = regexp.MustCompile(`\bfor\s*\(\s*int\s+(\w+)\s*=\s*([^;]+);([^;]+);([^)]*)\)`)
var shaderTernaryReturn = regexp.MustCompile(`\breturn\s+(\w+)\s*\?\s*([^:;]+)\s*:\s*([^;]+);`)
var shaderIdentifier = regexp.MustCompile(`\b[A-Za-z_]\w*\b`)

type shaderFunc struct{ result, args, body string }

// translateStageShader converts reachable Pen+ GLSL functions to Kage. Unused helpers are omitted;

// unsupported live syntax remains a compiler error.
func translateStageShader(source string) (string, map[string]string, error) {
	source = shaderComments.ReplaceAllString(source, "")
	functions := map[string]shaderFunc{}
	for _, loc := range shaderFunction.FindAllStringSubmatchIndex(source, -1) {
		end := loc[1]
		depth := 1
		for end < len(source) && depth > 0 {
			if source[end] == '{' {
				depth++
			}
			if source[end] == '}' {
				depth--
			}
			end++
		}
		if depth != 0 {
			return "", nil, fmt.Errorf("unterminated shader function")
		}
		name := source[loc[4]:loc[5]]
		functions[name] = shaderFunc{source[loc[2]:loc[3]], source[loc[6]:loc[7]], source[loc[1] : end-1]}
	}
	if _, ok := functions["main"]; !ok {
		return "", nil, fmt.Errorf("stage shader has no main function")
	}
	used := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if used[name] {
			return
		}
		used[name] = true
		for _, id := range shaderIdentifier.FindAllString(functions[name].body, -1) {
			if _, ok := functions[id]; ok {
				visit(id)
			}
		}
	}
	visit("main")
	uniforms := map[string]string{}
	var out strings.Builder
	out.WriteString("//kage:unit pixels\npackage main\nvar StageResolution vec2\nvar StageTime float\nvar StageElapsed float\nvar StageNearest int\n")
	replace := map[string]string{"gl_FragColor": "shaderColor", "fragColor": "shaderColor", "u_res": "StageResolution", "u_timer": "StageTime", "u_elapsed": "StageElapsed", "v_color": "vec4(1)", "v_texCoord": "vec2(StagePos.x / StageResolution.x, 1 - StagePos.y / StageResolution.y)", "gl_FragCoord": "vec4(StagePos.x, StageResolution.y - StagePos.y, 0, 1)"}
	for _, u := range shaderUniform.FindAllStringSubmatch(source, -1) {
		if u[2] == "u_skin" || u[2] == "u_res" || u[2] == "u_timer" || u[2] == "u_elapsed" {
			continue
		}
		uniforms[u[2]] = u[1]
		replace[u[2]] = "U_" + u[2]
		if u[1] == "sampler2D" {
			return "", nil, fmt.Errorf("stage shader texture %s is unsupported", u[2])
		}
		fmt.Fprintf(&out, "var U_%s %s\n", u[2], u[1])
	}
	out.WriteString(`func stageTexture(uv vec2) vec4 {
 size := imageSrc0Size()
 p := vec2(uv.x, 1-uv.y)*size
 if StageNearest != 0 { return imageSrc0At(imageSrc0Origin()+clamp(floor(p),vec2(0),size-1)+vec2(.5)) }
 p -= vec2(.5)
 base := floor(p)
 f := fract(p)
 a := imageSrc0At(imageSrc0Origin()+clamp(base,vec2(0),size-1)+vec2(.5))
 b := imageSrc0At(imageSrc0Origin()+clamp(base+vec2(1,0),vec2(0),size-1)+vec2(.5))
 c := imageSrc0At(imageSrc0Origin()+clamp(base+vec2(0,1),vec2(0),size-1)+vec2(.5))
 d := imageSrc0At(imageSrc0Origin()+clamp(base+vec2(1,1),vec2(0),size-1)+vec2(.5))
 return mix(mix(a,b,f.x),mix(c,d,f.x),f.y)
}
`)
	names := make([]string, 0, len(used))
	for n := range used {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f := functions[name]
		body := strings.Join(strings.Fields(shaderPrecision.ReplaceAllString(f.body, "")), " ")
		body = shaderFor.ReplaceAllString(body, "for $1 := $2;$3;$4")
		body = shaderConstant.ReplaceAllString(body, "const $1 = $2;")
		body = shaderDeclaration.ReplaceAllStringFunc(body, func(s string) string {
			m := shaderDeclaration.FindStringSubmatch(s)
			typ := m[1]
			if m[3] != "" {
				typ = "[" + m[3] + "]" + typ
			}
			return "var " + m[2] + " " + typ
		})
		body = shaderTernaryReturn.ReplaceAllString(body, "if $1 { return $2; }; return $3;")
		body = regexp.MustCompile(`\btexture(?:2D)?\s*\(\s*u_skin\s*,`).ReplaceAllString(body, "stageTexture(")
		body = shaderIdentifier.ReplaceAllStringFunc(body, func(s string) string {
			if r, ok := replace[s]; ok {
				return r
			}
			return s
		})
		// Kage uses Go's explicit statement semicolons/newlines. Keep GLSL's
		// semicolons and make closing-brace boundaries explicit as well.
		body = regexp.MustCompile(`}\s*else`).ReplaceAllString(body, "} else")
		body = regexp.MustCompile(`}\s*(else\b|[^\s;},])`).ReplaceAllStringFunc(body, func(s string) string {
			if strings.HasPrefix(strings.TrimSpace(s[1:]), "else") {
				return s
			}
			return "};" + s[1:]
		})
		for _, callee := range names {
			if callee == "main" {
				continue
			}
			body = addShaderPosition(body, callee)
		}
		if name == "main" {
			fmt.Fprintf(&out, "func shaderMain(StagePos vec2) vec4 { var shaderColor vec4; %s; return shaderColor }\n", strings.TrimRight(strings.TrimSpace(body), ";"))
		} else {
			args := []string{}
			for _, arg := range strings.Split(shaderPrecision.ReplaceAllString(f.args, ""), ",") {
				a := strings.Fields(arg)
				if len(a) == 2 {
					args = append(args, a[1]+" "+a[0])
				} else if len(a) != 0 {
					return "", nil, fmt.Errorf("unsupported shader argument %q", arg)
				}
			}
			args = append(args, "StagePos vec2")
			fmt.Fprintf(&out, "func %s(%s) %s { %s }\n", name, strings.Join(args, ","), f.result, body)
		}
	}
	out.WriteString("func Fragment(position vec4, texCoord vec2, color vec4) vec4 { return shaderMain(texCoord-imageSrc0Origin()) }\n")
	return out.String(), uniforms, nil
}

func addShaderPosition(body, name string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
	locations := re.FindAllStringIndex(body, -1)
	for i := len(locations) - 1; i >= 0; i-- {
		start := locations[i][1]
		end := start
		depth := 1
		for end < len(body) && depth > 0 {
			if body[end] == '(' {
				depth++
			}
			if body[end] == ')' {
				depth--
			}
			end++
		}
		if depth == 0 {
			separator := ","
			if strings.TrimSpace(body[start:end-1]) == "" {
				separator = ""
			}
			body = body[:end-1] + separator + "StagePos" + body[end-1:]
		}
	}
	return body
}
