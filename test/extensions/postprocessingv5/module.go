// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"math"
	"regexp"
	"sort"
	"strings"
)

func cmd(op string, inputs []string, fn e.Handler) e.Block {
	return e.Block{Opcode: "postprocessingv5_" + op, Kind: "command", Inputs: inputs, Handler: fn}
}

type pass struct {
	Name   string
	Params map[string]any
}

func passes(c *e.Context) []pass {
	p, _ := c.State["passes"].([]pass)
	return p
}
func ensure(c *e.Context) []pass {
	p := passes(c)
	i := int(e.Number(c.State["index"]))
	for len(p) <= i {
		p = append(p, pass{Name: "none"})
	}
	c.State["passes"] = p
	return p
}
func apply(c *e.Context) (e.Result, error) {
	names := []any{}
	initialized, _ := c.State["programs"].(map[string]bool)
	if initialized == nil {
		initialized = map[string]bool{}
		c.State["programs"] = initialized
	}
	for _, p := range passes(c) {
		if p.Name == "none" {
			continue
		}
		src, ok := shaders[p.Name]
		if !ok {
			return e.Result{}, fmt.Errorf("unsupported postprocessing effect %q", p.Name)
		}
		name := "postprocessingv5:" + p.Name
		if !initialized[p.Name] {
			if _, err := c.Call("graphics.shader.define", name, e.ShaderSource{Code: src, Compiler: ShaderCompiler{}}, defaults(p.Name)); err != nil {
				return e.Result{}, err
			}
			initialized[p.Name] = true
		}
		names = append(names, map[string]any{"name": name, "uniforms": p.Params})
	}
	_, err := c.Call("graphics.shader.pipeline", names)
	return e.Result{}, err
}
func defaults(name string) map[string]any {
	switch name {
	case "chromatic":
		return map[string]any{"power": .01, "sampleCount": 3}
	case "fisheye":
		return map[string]any{"power": -.1}
	case "pixelwarp":
		return map[string]any{"power": .03, "rate": 1, "speed": 5, "blockCount": 30.5, "colorRate": .01, "angle": 0}
	case "screensplit":
		return map[string]any{"split": []float64{1, 1}, "offset": []float64{0, 0}}
	case "frameborder":
		return map[string]any{"MAIN_HEIGHT": .89, "MAIN_ASPECT": 1.33, "TEXTURE_SCALE": 1.1238, "TEXTURE_ASPECT": 1.336, "BG_SCALE": 1.5, "BG_OFFSET": []float64{0, 0}, "BG_BRIGHTNESS": .6, "BG_SATURATION": 1, "BORDER_WIDTH": 2, "BORDER_COLOR": []float64{1, 1, 1}, "u_useBgTex": 0}
	}
	return map[string]any{}
}
func New() e.Extension {
	x := e.Extension{ID: "postprocessingv5", Blocks: []e.Block{
		{Opcode: "postprocessingv5_menu_PostProcess", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("PostProcess")}, nil }},
		cmd("opcodePipelineClear", nil, func(c *e.Context) (e.Result, error) {
			c.State["passes"] = []pass{}
			return apply(c)
		}),
		cmd("opcodePipelineAdd", []string{"Menu"}, func(c *e.Context) (e.Result, error) {
			n := c.String("Menu")
			if n == "画中画边框" {
				n = "frameborder"
			}
			if n != "none" && n != "pipeline" {
				if _, ok := shaders[n]; !ok {
					return e.Result{}, fmt.Errorf("unsupported postprocessing effect %q", n)
				}
				c.State["passes"] = append(passes(c), pass{Name: n})
			}
			return apply(c)
		}),
		cmd("opcodePipelineSetTargetIndex", []string{"index"}, func(c *e.Context) (e.Result, error) {
			i := math.Max(0, math.Floor(c.Number("index")))
			if math.IsInf(i, 0) || math.IsNaN(i) || i > 65535 {
				return e.Result{}, fmt.Errorf("invalid pipeline index")
			}
			c.State["index"] = i
			ensure(c)
			return apply(c)
		}),
		{Opcode: "postprocessingv5_opcodeGetPipeline", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			n := []string{}
			for _, p := range passes(c) {
				n = append(n, p.Name)
			}
			return e.Result{Value: strings.Join(n, ",")}, nil
		}},
	}}
	for _, spec := range []struct {
		op, name         string
		inputs, uniforms []string
	}{
		{"opcodeChangeChromatic", "chromatic", []string{"c_power", "c_samplecount"}, []string{"power", "sampleCount"}},
		{"opcodeChangeFisheye", "fisheye", []string{"f_power"}, []string{"power"}},
		{"opcodeChangePixelWarp", "pixelwarp", []string{"pw_power", "pw_blockcount", "pw_colorrate", "pw_angle"}, []string{"power", "blockCount", "colorRate", "angle"}},
		{"opcodeChangeScreensplit", "screensplit", []string{"split_x", "split_y"}, nil},
	} {
		x.Blocks = append(x.Blocks, cmd(spec.op, spec.inputs, func(c *e.Context) (e.Result, error) {
			p := ensure(c)
			i := int(e.Number(c.State["index"]))
			u := map[string]any{}
			for j, k := range spec.uniforms {
				u[k] = c.Number(spec.inputs[j])
			}
			if spec.name == "screensplit" {
				u["split"] = []float64{c.Number("split_x"), c.Number("split_y")}
			}
			p[i] = pass{Name: spec.name, Params: u}
			c.State["passes"] = p
			return apply(c)
		}))
	}
	return x
}

var shaders = map[string]string{
	"chromatic": `
    precision mediump float;
    varying vec2 v_texCoord;
    varying vec4 vColor;
    uniform sampler2D u_skin;
    uniform float power;
    uniform float sampleCount;
    void main() {
      vec3 sum = vec3(0.0);
      vec3 c = vec3(0.0);
      vec2 offset = (v_texCoord - vec2(0.5)) * vec2(1, -1);
      int sample_count = int(sampleCount);
      for (int i = 0; i < 4; i++) {
        if (i >= sample_count) {break;}
        float t = 2.0 * float(i) / float(sample_count - 1);
        vec3 slice = vec3(1.0 - t, 1.0 - abs(t - 1.0), t - 1.0);
        slice = max(slice, 0.0);
        sum += slice;
        vec2 slice_offset = (t - 1.0) * power * offset;
        c += slice * texture2D(u_skin, v_texCoord + slice_offset).rgb;
      }
      gl_FragColor.rgb = c / sum;
      gl_FragColor.a = 1.0;
    }
  `,
	"fisheye": `
    precision mediump float;
    varying vec2 v_texCoord;
    varying vec4 vColor;
    uniform sampler2D u_skin;
    uniform vec2 u_res;
    uniform float power;
    void main() {
      vec2 p = vec2(v_texCoord.x, v_texCoord.y * u_res.y / u_res.x);
      float aspect = u_res.x / u_res.y;
      vec2 m = vec2(0.5, 0.5 / aspect);
      vec2 d = p - m;
      float r = sqrt(dot(d, d));
      float new_power = (2.0 * 3.141592 / (2.0 * sqrt(dot(m, m)))) * power;
      float bind = m.y; if (aspect < 1.0) {bind=m.x;} if (new_power > 0.0) {bind=sqrt(dot(m,m));}
      vec2 nuv;
      if (new_power > 0.0) {
        nuv = m + normalize(d) * tan(r * new_power) * bind / tan(bind * new_power);
      } else {
        nuv = m + normalize(d) * atan(r * -new_power * 10.0) * bind / atan(-new_power * bind * 10.0);
      }
      if (power == 0.0) {nuv=p;} gl_FragColor = texture2D(u_skin, vec2(nuv.x, nuv.y * aspect));
    }
  `,
	"pixelwarp": `
    precision mediump float;
    varying vec2 v_texCoord;
    varying vec4 vColor;
    uniform sampler2D u_skin;
    uniform float u_elapsed;
    uniform float power;
    uniform float rate;
    uniform float speed;
    uniform float blockCount;
    uniform float colorRate;
    uniform float angle; // degrees
    float my_trunc(float x) {
      if (x < 0.0) {return -floor(-x);} return floor(x);
    }
    float random(float seed) {
      return fract(543.2543 * sin(dot(vec2(seed, seed), vec2(3525.46, -54.3415))));
    }
    void main() {
      float enable_shift = 0.0; if (random(my_trunc(u_elapsed * speed)) < rate) {enable_shift=1.0;}
      float a = (angle * 0.017453292519943295);
      vec2 D = vec2(cos(a), sin(a));
      vec2 N = vec2(-sin(a), cos(a));
      float stripeCoord = dot(v_texCoord, N);
      float blockIdx = my_trunc(stripeCoord * blockCount);
      float shiftAmount = (random((blockIdx / blockCount) + u_elapsed) - 0.5) * power * enable_shift;
      vec2 fixed_uv = v_texCoord + D * shiftAmount;
      vec4 pixel_color = texture2D(u_skin, fixed_uv);
      pixel_color.r = mix(
        pixel_color.r,
        texture2D(u_skin, fixed_uv + D * colorRate).r,
        enable_shift
      );
      pixel_color.b = mix(
        pixel_color.b,
        texture2D(u_skin, fixed_uv - D * colorRate).b,
        enable_shift
      );
      gl_FragColor = pixel_color;
    }
  `,
	"screensplit": `
      precision mediump float;
      varying vec2 v_texCoord;
      varying vec4 vColor;      
      uniform sampler2D u_skin;
      uniform vec2 split ;
      uniform vec2 offset ;
      vec2 repeatUV(vec2 suv, float min, float max) {
        return mod(suv, max - min) + min;
    }
      void main() {
        vec4 col = texture2D(u_skin,repeatUV(v_texCoord / split + offset,0.0,1.0)) ;
        gl_FragColor=col;
      }
    `,
	"frameborder": `
precision highp float;
varying vec2 v_texCoord;
uniform sampler2D u_skin;

uniform float u_useBgTex;
uniform vec2 u_res;
uniform float u_elapsed;

uniform float MAIN_HEIGHT;
uniform float MAIN_ASPECT;
uniform float TEXTURE_SCALE;
uniform float TEXTURE_ASPECT;
uniform float BG_SCALE;
uniform vec2  BG_OFFSET;
uniform float BG_BRIGHTNESS;
uniform float BG_SATURATION;
uniform float BORDER_WIDTH;
uniform vec3  BORDER_COLOR;

vec3 rgb2hsv(vec3 c) {
    vec4 K = vec4(0.0, -1.0/3.0, 2.0/3.0, -1.0);
    vec4 p = mix(vec4(c.bg, K.wz), vec4(c.gb, K.xy), step(c.b, c.g));
    vec4 q = mix(vec4(p.xyw, c.r), vec4(c.r, p.yzx), step(p.x, c.r));
    float d = q.x - min(q.w, q.y);
    float e = 1.0e-10;
    return vec3(abs(q.z + (q.w - q.y)/(6.0*d + e)), d/(q.x + e), q.x);
}
vec3 hsv2rgb(vec3 c) {
    vec4 K = vec4(1.0, 2.0/3.0, 1.0/3.0, 3.0);
    vec3 p = abs(fract(c.xxx + K.xyz)*6.0 - K.www);
    return c.z * mix(K.xxx, clamp(p - K.xxx, 0.0, 1.0), c.y);
}

void main() {
    vec2 uv = v_texCoord;

    vec2 processedUV = (uv - 0.5) / TEXTURE_SCALE + 0.5;
    processedUV.x = (processedUV.x - 0.5) / TEXTURE_ASPECT + 0.5;

    vec2 centerUV = uv - 0.5;
    float mainH = MAIN_HEIGHT;
    float mainW = mainH * MAIN_ASPECT;
    float screenAspect = u_res.x / u_res.y;
    centerUV.x *= screenAspect;

    float borderUVWidth = BORDER_WIDTH / u_res.y;
    float innerX = mainW * 0.5;
    float innerY = mainH * 0.5;
    float outerX = innerX + borderUVWidth;
    float outerY = innerY + borderUVWidth;

    float inBorder = step(abs(centerUV.x), outerX) * step(abs(centerUV.y), outerY)
                   * (1.0 - step(abs(centerUV.x), innerX) * step(abs(centerUV.y), innerY));
    float inMainArea = step(abs(centerUV.x), innerX) * step(abs(centerUV.y), innerY);

    vec2 mainUV = vec2(
        (centerUV.x / mainW) + 0.5,
        (centerUV.y / mainH) + 0.5
    );
    mainUV = (mainUV - 0.5) / TEXTURE_SCALE + 0.5;
    mainUV.x = (mainUV.x - 0.5) / TEXTURE_ASPECT + 0.5;
    vec3 mainCol = texture2D(u_skin, mainUV).rgb;

    vec2 bgUV;
    if (u_useBgTex < 0.5) {
        bgUV = (uv - 0.5 - BG_OFFSET) / BG_SCALE + 0.5;
    } else {
        bgUV = uv;
        bgUV.y = 1.0 - bgUV.y;
    }

    vec3 bgCol;
    if (u_useBgTex < 0.5) {
        bgCol = texture2D(u_skin, bgUV).rgb;
        vec3 bgHSV = rgb2hsv(bgCol);
        bgHSV.y *= BG_SATURATION;
        bgCol = hsv2rgb(bgHSV) * BG_BRIGHTNESS;
    } else {
        bgCol = texture2D(u_skin, bgUV).rgb;
    }

    vec3 finalCol = mix(
        mix(bgCol, mainCol, inMainArea),
        BORDER_COLOR,
        inBorder
    );

    gl_FragColor = vec4(finalCol, 1.0);
}
`,
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
