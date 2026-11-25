// Package packager is the graphical project packager.
package packager

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font"

	"gowarp/app/assets"
	"gowarp/app/compiler"
	"gowarp/app/config"
	"gowarp/app/dialog"
	"gowarp/app/editorbridge"
	"gowarp/app/host"
	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/project"
	"gowarp/core/vm"
)

type hit struct {
	rect   image.Rectangle
	action func()
	edit   *string
}

type result struct {
	closedEditor                           *editorbridge.Server
	importedExtension                      *extensionImport
	extensions                             *extensionEditor
	savedExtensions                        *compiler.ExtensionDocument
	committed                              bool
	icon                                   []byte
	loadingData                            []byte
	loadingImage                           *assets.LoadingImage
	loadingKey                             string
	selectedPath, selectMode, fallbackMode string
	browser                                *browser
	status                                 string
	form                                   *form
	preview                                *preview
	err                                    error
}

type browser struct {
	path, name, mode string
	entries          []os.DirEntry
	scroll           int
}

type application struct {
	editorBridge    *editorbridge.Server
	editorScreen    bool
	editorStatus    editorbridge.Status
	quit            bool
	extensions      *extensionEditor
	scale           float64
	ctx             context.Context
	cancel          context.CancelFunc
	form            *form
	section, scroll int
	face, title     font.Face
	pixels          *image.RGBA
	canvas          *ebiten.Image
	width, height   int
	dirty           bool
	hits            []hit
	editor          *string
	cursor          int
	selectAll       bool
	keyboardFocus   int
	status          string
	busy            bool
	jobStarted      time.Time
	jobCancel       context.CancelFunc
	results         chan result
	clipboard       chan clipboardResult
	templates       []compiler.Template
	target          int
	preview         *preview
	browser         *browser
	lastDir         string
	errorText       string
	dropdown        *dropdown
	languagePath    string
}

func Main() {
	a, err := newApplication()
	if err != nil {
		ui.ShowMessage("GoWarp", err.Error())
		return
	}
	defer a.cancel()
	defer func() {
		if a.editorBridge != nil {
			a.editorBridge.Close()
		}
	}()
	defer func() {
		a.face.Close()
		a.title.Close()
	}()
	defer func() {
		if a.preview != nil {
			a.preview.cancel()
			<-a.preview.Done
		}
	}()
	ebiten.SetWindowTitle("GoWarp Packager")
	ebiten.SetWindowIcon(assets.DefaultWindowIcon())
	ebiten.SetWindowSize(840, 620)
	ebiten.SetWindowSizeLimits(840, 620, 840, 620)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	ebiten.SetWindowClosingHandled(true)
	ebiten.SetRunnableOnUnfocused(true)
	ebiten.SetTPS(30)
	if err := ebiten.RunGame(a); err != nil && err != ebiten.Termination {
		fmt.Fprintln(os.Stderr, err)
	}
}

func newApplication() (*application, error) {
	ctx, cancel := context.WithCancel(context.Background())
	home, err := os.UserHomeDir()
	if err != nil {
		cancel()
		return nil, err
	}
	preferences, err := os.UserConfigDir()
	if err != nil {
		cancel()
		return nil, err
	}
	languagePath := filepath.Join(preferences, "gowarp", "preferences.json")
	languageErr := loadLanguage(languagePath)
	a := &application{
		ctx:           ctx,
		cancel:        cancel,
		form:          newForm(config.Default(nil), nil),
		face:          ui.Font(16),
		title:         ui.Font(26),
		dirty:         true,
		status:        i18n.Tr("status.open_hint"),
		results:       make(chan result, 1),
		lastDir:       home,
		keyboardFocus: -1,
		languagePath:  languagePath,
	}
	if languageErr != nil {
		a.status = languageErr.Error()
	}
	exe, err := os.Executable()
	if err != nil {
		return a, err
	}
	for _, dir := range []string{filepath.Join(filepath.Dir(exe), "runtimes"), filepath.Join(filepath.Dir(exe), "..", "runtimes")} {
		a.templates, err = compiler.Templates(dir)
		if err != nil {
			a.status = err.Error()
			continue
		}
		if len(a.templates) > 0 {
			break
		}
	}
	for i, t := range a.templates {
		if t.OS == runtime.GOOS && t.Arch == runtime.GOARCH {
			a.target = i
		}
	}
	return a, nil
}

func (a *application) job(label string, work func(context.Context) result) {
	if a.busy {
		return
	}
	a.busy = true
	a.jobStarted = time.Now()
	a.status = label
	a.editor = nil
	a.closeDropdown()
	a.hits = nil
	a.keyboardFocus = -1
	a.dirty = true
	ctx, cancel := context.WithCancel(a.ctx)
	a.jobCancel = cancel
	go func() {
		r := work(ctx)
		if err := ctx.Err(); err != nil && !r.committed {
			if r.preview != nil {
				r.preview.cancel()
				<-r.preview.Done
			}
			r = result{err: err}
		}
		cancel()
		select {
		case a.results <- r:
		case <-a.ctx.Done():
			if r.preview != nil {
				r.preview.cancel()
				<-r.preview.Done
			}
		}
	}()
}

func (a *application) openProject(path string) {
	a.job(i18n.Tr("job.open_project"), func(ctx context.Context) result {
		path, err := filepath.Abs(path)
		if err != nil {
			return result{err: err}
		}
		p, err := project.Load(path)
		if err != nil {
			return result{err: err}
		}
		if err = ctx.Err(); err != nil {
			return result{err: err}
		}
		s := config.DefaultForProject(path, p)
		return result{
			form:   newForm(s, variableNames(p)),
			status: i18n.Tr("status.project_loaded", filepath.Base(path), s.Width, s.Height, s.FPS, displayChoice(fmt.Sprint(s.Turbo))),
		}
	})
}

func variableNames(p *project.Project) map[string]string {
	names := map[string]string{}
	for id, v := range vm.New(p).CloudValues() {
		names[id] = v.Name
	}
	return names
}

func (a *application) loadSettings(path string) {
	a.job(i18n.Tr("job.open_settings"), func(ctx context.Context) result {
		if err := ctx.Err(); err != nil {
			return result{err: err}
		}
		s, err := config.Load(path)
		if err != nil {
			return result{err: err}
		}
		var names map[string]string
		if s.Project != "" {
			p, e := project.Load(s.Project)
			if e != nil {
				return result{err: e}
			}
			names = variableNames(p)
		}
		if err := ctx.Err(); err != nil {
			return result{err: err}
		}
		return result{form: newForm(s, names), status: i18n.Tr("status.settings_loaded")}
	})
}

func (a *application) targetOS() string {
	if a.target < 0 || a.target >= len(a.templates) {
		return ""
	}
	return a.templates[a.target].OS
}

func (a *application) runPreview() {
	if a.busy || a.preview != nil {
		return
	}
	f := *a.form
	f.fields = append([]field(nil), f.fields...)
	for _, t := range a.templates {
		if t.OS == runtime.GOOS && t.Arch == runtime.GOARCH {
			a.job(i18n.Tr("job.preview"), func(ctx context.Context) result {
				s, err := f.settings()
				if err != nil {
					return result{err: err}
				}
				s = settingsForPlatform(s, t.OS)
				p, err := startPreview(ctx, a.ctx, t, s.Project, s)
				return result{
					preview: p,
					err:     err,
					status:  i18n.Tr("status.preview"),
				}
			})
			return
		}
	}
	a.status = i18n.Tr("status.no_preview_runtime")
	a.dirty = true
}

func (a *application) save(path string, export bool) {
	if a.busy {
		return
	}
	if export && !a.requireProject() {
		return
	}
	f := *a.form
	f.fields = append([]field(nil), f.fields...)
	var tpl compiler.Template
	if export {
		if len(a.templates) == 0 {
			a.status = i18n.Tr("status.no_runtime")
			return
		}
		tpl = a.templates[a.target]
	}
	label := i18n.Tr("job.save")
	if export {
		label = i18n.Tr("job.export")
	}
	a.job(label, func(ctx context.Context) result {
		s, err := f.settings()
		if err != nil {
			return result{err: err}
		}
		if export {
			s = settingsForPlatform(s, tpl.OS)
			err = compiler.Export(ctx, tpl, s.Project, path, s)
		} else {
			if s.Project != "" {
				err = compiler.ValidateOutputs(s.Project, nil, path)
			}
			if err == nil {
				err = config.SaveContext(ctx, path, s)
			}
		}
		return result{err: err, status: i18n.Tr("status.saved", path)}
	})
}

func (a *application) requireProject() bool {
	for _, field := range a.form.fields {
		if field.key == "project" && strings.TrimSpace(field.value) != "" {
			return true
		}
	}
	a.errorText = i18n.Tr("error.project_required")
	a.dirty = true
	return false
}

func (a *application) choose(mode string) {
	if a.busy {
		return
	}
	if mode == "export" && !a.requireProject() {
		return
	}
	a.editor = nil
	options := dialog.Options{
		Title:     i18n.Tr("dialog.project"),
		Directory: a.lastDir,
		Patterns:  []string{"*.sb3", "*.json"},
	}
	switch mode {
	case "extension":
		options.Title = i18n.Tr("extensions.import")
		options.Patterns = []string{"*.js"}
	case "icon":
		options.Title = i18n.Tr("dialog.icon")
		options.Patterns = []string{"*.png", "*.jpg", "*.jpeg"}
	case "loading.logo", "loading.image":
		options.Title = i18n.Tr("dialog.loading_image")
		options.Patterns = []string{"*.png", "*.jpg", "*.jpeg", "*.gif"}
	case "open":
		options.Title = i18n.Tr("action.open_settings")
		options.Patterns = []string{"*.gowarp.json", "*.json"}
	case "save":
		options.Title = i18n.Tr("action.save_settings")
		options.Save = true
		options.Filename = "project.gowarp.json"
		options.Patterns = []string{"*.gowarp.json"}
	case "export":
		options.Title = i18n.Tr("dialog.save_executable")
		options.Save = true
		options.Filename = "game"
		options.Patterns = nil
		if len(a.templates) > 0 && a.templates[a.target].OS == "windows" {
			options.Filename += ".exe"
		}
	}
	a.job(i18n.Tr("job.select"), func(ctx context.Context) result {
		path, err := dialog.Select(ctx, options)
		if errors.Is(err, dialog.ErrCancelled) {
			return result{status: i18n.Tr("status.selection_canceled")}
		}
		if errors.Is(err, dialog.ErrUnavailable) {
			return result{
				fallbackMode: mode,
				status:       i18n.Tr("status.fallback_browser"),
			}
		}
		return result{
			selectedPath: path,
			selectMode:   mode,
			err:          err,
		}
	})
}

func (a *application) acceptPath(mode, path string) {
	a.lastDir = filepath.Dir(path)
	switch mode {
	case "extension":
		a.importExtensionFile(path)
	case "icon":
		a.loadIcon(path)
	case "loading.logo", "loading.image":
		a.loadLoadingImage(mode, path)
	case "project":
		a.openProject(path)
	case "open":
		a.loadSettings(path)
	case "save":
		a.save(path, false)
	case "export":
		a.save(path, true)
	}
}

func (a *application) chooseFallback(mode string) {
	if a.busy {
		return
	}
	a.editor = nil
	a.browser = &browser{path: a.lastDir, mode: mode}
	if mode == "save" {
		a.browser.name = "project.gowarp.json"
	}
	if mode == "export" {
		a.browser.name = "game"
		if len(a.templates) > 0 && a.templates[a.target].OS == "windows" {
			a.browser.name += ".exe"
		}
	}
	a.readDirectory()
}

func (a *application) readDirectory() {
	if a.busy || a.browser == nil {
		return
	}
	requested := *a.browser
	a.editor = nil
	a.job(i18n.Tr("job.directory"), func(ctx context.Context) result {
		path, err := filepath.Abs(requested.path)
		if err != nil {
			return result{err: err}
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return result{err: err}
		}
		if err = ctx.Err(); err != nil {
			return result{err: err}
		}
		requested.path = path
		requested.entries = entries
		requested.scroll = 0
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].IsDir() && !entries[j].IsDir()
		})
		return result{browser: &requested, status: path}
	})
}

func (a *application) acceptFile() {
	if a.busy {
		return
	}
	b := a.browser
	if b == nil {
		return
	}
	path := b.path
	if b.name != "" {
		path = filepath.Join(b.path, b.name)
	}
	if filepath.IsAbs(b.name) {
		path = b.name
	}
	mode := b.mode
	a.browser = nil
	a.editor = nil
	a.dirty = true
	switch mode {
	case "extension":
		a.importExtensionFile(path)
	case "icon":
		a.loadIcon(path)
	case "loading.logo", "loading.image":
		a.loadLoadingImage(mode, path)
	case "project":
		a.openProject(path)
	case "open":
		a.loadSettings(path)
	case "save":
		a.save(path, false)
	case "export":
		a.save(path, true)
	}
}

func (a *application) Update() error {
	if a.quit || (ebiten.IsWindowBeingClosed() && a.requestWindowClose()) {
		return ebiten.Termination
	}
	a.pollClipboard()
	if a.editorBridge != nil {
		status := a.editorBridge.Status()
		if status != a.editorStatus {
			a.editorStatus = status
			a.dirty = true
		}
	}
	select {
	case r := <-a.results:
		a.busy = false
		a.jobCancel = nil
		a.dirty = true
		if r.err != nil {
			a.closeDropdown()
			a.status = r.err.Error()
			if errors.Is(r.err, context.Canceled) {
				a.status = i18n.Tr("status.canceled")
			} else {
				a.errorText = r.err.Error()
			}
		} else {
			if r.closedEditor != nil && a.editorBridge == r.closedEditor {
				a.editorBridge = nil
			}
			a.status = r.status
			a.applyExtensionImport(r.importedExtension)
			if r.extensions != nil {
				a.extensions = r.extensions
				a.keyboardFocus = -1
			}
			if r.savedExtensions != nil && a.extensions != nil {
				a.extensions.document = r.savedExtensions
				a.extensions.message = i18n.Tr("extensions.saved")
			}
			if r.icon != nil {
				a.form.base.Player.Icon = r.icon
				a.form.icon, _ = assets.DecodeIcon(r.icon)
			}
			if r.loadingKey != "" {
				a.form.setLoadingImage(r.loadingKey, r.loadingData, r.loadingImage)
			}
			if r.fallbackMode != "" {
				a.chooseFallback(r.fallbackMode)
			}
			if r.selectedPath != "" {
				a.acceptPath(r.selectMode, r.selectedPath)
			}
			if r.browser != nil && a.browser != nil {
				a.browser = r.browser
				a.lastDir = r.browser.path
				a.editor = nil
			}
			if r.form != nil {
				a.closeDropdown()
				a.form = r.form
				a.editor = nil
				a.scroll = 0
			}
			if r.preview != nil {
				a.preview = r.preview
			}
		}
	default:
	}
	if a.preview != nil {
		select {
		case err := <-a.preview.Done:
			a.preview = nil
			a.dirty = true
			if !a.busy {
				a.status = i18n.Tr("status.preview_ended")
				if err != nil {
					a.status = err.Error()
				}
			}
		default:
		}
	}
	if a.busy {
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			a.cancelJob()
		}
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			x, y := ebiten.CursorPosition()
			x, y = a.logicalPoint(x, y)
			for _, h := range a.hits {
				if image.Pt(x, y).In(h.rect) && h.action != nil {
					h.action()
					break
				}
			}
		}
		return nil
	}
	if a.extensions != nil && a.browser == nil {
		a.updateExtensionEditor()
		return nil
	}
	if a.dropdown != nil {
		a.updateDropdown()
		return nil
	}
	if dropped := ebiten.DroppedFiles(); dropped != nil && !a.busy && a.extensions == nil && !a.editorScreen {
		entries, err := fs.ReadDir(dropped, ".")
		if err == nil && len(entries) > 0 {
			if p, ok := entries[0].(ebiten.AbsPather); ok {
				a.openProject(p.AbsPath())
			}
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		if a.errorText != "" {
			a.errorText = ""
		} else if a.browser != nil {
			a.browser = nil
			a.editor = nil
		} else if a.editorScreen {
			a.editorScreen = false
			a.keyboardFocus = -1
		} else if a.editor != nil {
			a.editor = nil
		} else if a.busy && a.jobCancel != nil {
			a.jobCancel()
		}
		a.dirty = true
	}
	_, wheel := ebiten.Wheel()
	if wheel != 0 {
		a.editor = nil
		if a.browser != nil {
			a.browser.scroll = max(0, min(max(0, len(a.browser.entries)-8), a.browser.scroll-int(wheel)*3))
		} else {
			a.scroll = max(0, a.scroll-int(wheel)*48)
		}
		a.dirty = true
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		x, y = a.logicalPoint(x, y)
		a.editor = nil
		for i, h := range a.hits {
			if image.Pt(x, y).In(h.rect) {
				a.keyboardFocus = i
				if h.action != nil {
					h.action()
				}
				break
			}
		}
		a.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) && len(a.hits) > 0 {
		delta := 1
		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			delta = -1
		}
		a.keyboardFocus = (a.keyboardFocus + delta + len(a.hits)) % len(a.hits)
		a.editor = nil
		h := a.hits[a.keyboardFocus]
		if h.edit != nil {
			a.focus(h.edit)
		}
		a.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		a.activateEnter()
	}
	if a.editor != nil {
		a.edit()
	}
	if a.extensions == nil && !a.editorScreen && a.browser == nil && a.errorText == "" {
		if inpututil.IsKeyJustPressed(ebiten.KeyF5) {
			a.runPreview()
		}
	}
	return nil
}

func (a *application) activateEnter() {
	if a.dropdown != nil {
		a.selectDropdown(a.dropdown.selected)
		return
	}
	if a.editor == nil && a.keyboardFocus >= 0 && a.keyboardFocus < len(a.hits) {
		if action := a.hits[a.keyboardFocus].action; action != nil {
			action()
		}
		return
	}
	if a.browser != nil {
		if a.editor == &a.browser.path {
			a.readDirectory()
		} else {
			a.acceptFile()
		}
	}
}

func (a *application) focus(value *string) {
	a.editor = value
	a.cursor = utf8.RuneCountInString(*value)
	a.selectAll = true
}

func (a *application) normalizePathInput() {
	if a.editor == nil {
		return
	}
	isPath := a.browser != nil && (a.editor == &a.browser.path || a.editor == &a.browser.name)
	if a.form != nil {
		for i := range a.form.fields {
			field := &a.form.fields[i]
			if field.key == "project" && a.editor == &field.value {
				isPath = true
				break
			}
		}
	}
	value := *a.editor
	if isPath && len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		*a.editor = value[1 : len(value)-1]
		a.cursor = max(0, min(a.cursor-1, utf8.RuneCountInString(*a.editor)))
	}
}

func repeated(key ebiten.Key) bool {
	n := inpututil.KeyPressDuration(key)
	return n == 1 || (n > 12 && n%2 == 0)
}

func (a *application) edit() {
	runes := []rune(*a.editor)
	a.cursor = max(0, min(a.cursor, len(runes)))
	if a.clipboardShortcut() {
		return
	}
	if repeated(ebiten.KeyArrowLeft) {
		a.cursor = max(0, a.cursor-1)
		a.selectAll = false
		a.dirty = true
	}
	if repeated(ebiten.KeyArrowRight) {
		a.cursor = min(len(runes), a.cursor+1)
		a.selectAll = false
		a.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
		a.cursor = 0
		a.selectAll = false
		a.dirty = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		a.cursor = len(runes)
		a.selectAll = false
		a.dirty = true
	}
	chars := ebiten.AppendInputChars(nil)
	back, del := repeated(ebiten.KeyBackspace), repeated(ebiten.KeyDelete)
	if len(chars) == 0 && !back && !del {
		return
	}
	if a.selectAll {
		runes = nil
		a.cursor = 0
		a.selectAll = false
	} else if back && a.cursor > 0 {
		runes = append(runes[:a.cursor-1], runes[a.cursor:]...)
		a.cursor--
	} else if del && a.cursor < len(runes) {
		runes = append(runes[:a.cursor], runes[a.cursor+1:]...)
	}
	if len(runes)+len(chars) <= 4096 {
		tail := append([]rune(nil), runes[a.cursor:]...)
		runes = append(runes[:a.cursor], chars...)
		runes = append(runes, tail...)
		a.cursor += len(chars)
	}
	*a.editor = string(runes)
	a.normalizePathInput()
	a.dirty = true
}

func (a *application) Layout(w, h int) (int, int) {
	return w, h
}

func (a *application) LayoutF(w, h float64) (float64, float64) {
	return a.layoutScale(w, h, ebiten.Monitor().DeviceScaleFactor())
}

func (a *application) layoutScale(w, h, scale float64) (float64, float64) {
	if scale <= 0 {
		scale = 1
	}
	if a.scale != scale {
		if a.face != nil {
			a.face.Close()
		}
		if a.title != nil {
			a.title.Close()
		}
		a.face = ui.Font(16 * scale)
		a.title = ui.Font(26 * scale)
		a.scale = scale
		a.dirty = true
	}
	if a.width != int(w) || a.height != int(h) {
		a.closeDropdown()
		a.width = int(w)
		a.height = int(h)
		a.dirty = true
	}
	return math.Ceil(w * scale), math.Ceil(h * scale)
}

func (a *application) pixelRect(r image.Rectangle) image.Rectangle {
	return image.Rect(
		int(math.Round(float64(r.Min.X)*a.scale)),
		int(math.Round(float64(r.Min.Y)*a.scale)),
		int(math.Round(float64(r.Max.X)*a.scale)),
		int(math.Round(float64(r.Max.Y)*a.scale)),
	)
}

func (a *application) logicalPoint(x, y int) (int, int) {
	if a.scale <= 0 {
		return x, y
	}
	return int(float64(x) / a.scale), int(float64(y) / a.scale)
}

func (a *application) fill(r image.Rectangle, c color.Color) {
	ui.Fill(a.pixels, a.pixelRect(r), c)
}

func (a *application) textWidth(s string) int {
	return int(math.Ceil(float64(font.MeasureString(a.face, s)) / 64 / a.scale))
}

func (a *application) Draw(screen *ebiten.Image) {
	if a.section == 5 && a.form != nil && !a.busy {
		now := time.Now()
		for _, picture := range []*assets.LoadingImage{a.form.loadingLogo, a.form.loadingImage} {
			changed, err := picture.Advance(now)
			if err != nil {
				a.status = err.Error()
				a.dirty = true
			}
			a.dirty = a.dirty || changed
		}
	}
	if a.pixels == nil || a.pixels.Bounds() != screen.Bounds() {
		a.pixels = image.NewRGBA(screen.Bounds())
		if a.canvas != nil {
			a.canvas.Deallocate()
		}
		a.canvas = ebiten.NewImage(screen.Bounds().Dx(), screen.Bounds().Dy())
		a.dirty = true
	}
	if a.dirty {
		a.render()
		a.canvas.WritePixels(a.pixels.Pix)
		a.dirty = false
	}
	screen.DrawImage(a.canvas, nil)
	if a.busy {
		bar := a.pixelRect(a.progressRect())
		segment := max(1, bar.Dx()/4)
		phase := math.Mod(time.Since(a.jobStarted).Seconds(), 1.6) / 1.6
		x := float64(bar.Min.X) + float64(bar.Dx()-segment)*(1-math.Cos(phase*2*math.Pi))/2
		vector.DrawFilledRect(screen, float32(x), float32(bar.Min.Y), float32(segment), float32(bar.Dy()), ui.Accent, false)
	}
}

func (a *application) label(x, y int, s string, c color.Color) {
	ui.Label(a.pixels, a.face, int(math.Round(float64(x)*a.scale)), int(math.Round(float64(y)*a.scale)), s, c)
}

func (a *application) button(x, y, w int, text string, action func()) {
	a.coloredButton(x, y, w, text, color.RGBA{51, 51, 51, 255}, action)
}

func (a *application) coloredButton(x, y, w int, text string, background color.Color, action func()) {
	rect := image.Rect(x, y, x+w, y+34)
	a.fill(rect, background)
	a.label(x+10, y+23, a.fitText(text, w-20), ui.Text)
	a.hits = append(a.hits, hit{rect: rect, action: action})
}

func (a *application) input(x, y, w int, value *string) {
	rect := image.Rect(x, y, x+w, y+34)
	a.fill(rect, ui.Field)
	text := *value
	if a.editor == value {
		a.fill(image.Rect(x, y+32, x+w, y+34), ui.Accent)
		r := []rune(text)
		cursor := min(a.cursor, len(r))
		text = string(r[:cursor]) + "|" + string(r[cursor:])
		if a.selectAll {
			a.fill(rect.Inset(2), color.RGBA{75, 45, 48, 255})
		}
	}
	for len(text) > 0 && a.textWidth(text) > w-16 {
		_, n := utf8.DecodeRuneInString(text)
		text = text[n:]
	}
	a.label(x+8, y+23, text, ui.Text)
	a.hits = append(a.hits, hit{
		rect: rect,
		action: func() {
			a.focus(value)
		},
		edit: value,
	})
}

var sections = []string{"section.project", "section.runtime", "section.window", "section.player", "section.cloud", "section.loading", "section.platform", "section.extensions"}

func (a *application) render() {
	a.hits = nil
	ui.Fill(a.pixels, a.pixels.Bounds(), ui.Background)
	if a.extensions != nil {
		a.renderExtensionEditor()
	} else if a.editorScreen {
		a.renderTurboWarpScreen()
	} else {
		a.renderPackager()
	}
	if a.dropdown != nil && a.browser == nil && a.errorText == "" {
		a.renderDropdown()
	}
	if a.browser != nil {
		a.renderBrowser()
	}
	if a.errorText != "" {
		a.hits = nil
		a.modalFrame(image.Rect(60, 110, a.width-60, a.height-100))
		a.label(84, 148, i18n.Tr("error.title"), ui.Accent)
		line := ""
		y := 184
		for _, r := range a.errorText {
			if r == '\n' || a.textWidth(line+string(r)) > a.width-168 {
				a.label(84, y, line, ui.Text)
				line = ""
				y += 24
				if y > a.height-180 {
					break
				}
				if r == '\n' {
					continue
				}
			}
			line += string(r)
		}
		a.label(84, y, line, ui.Text)
		a.button(84, a.height-150, 120, i18n.Tr("action.close"), func() {
			a.errorText = ""
		})
	}
	if a.busy {
		a.renderBusy()
	}
	if a.keyboardFocus >= 0 && a.keyboardFocus < len(a.hits) {
		r := a.hits[a.keyboardFocus].rect
		a.fill(image.Rect(r.Min.X, r.Max.Y-2, r.Max.X, r.Max.Y), ui.Accent)
	}
}

func (a *application) renderPackager() {
	ui.Label(a.pixels, a.title, int(24*a.scale), int(42*a.scale), "GoWarp", ui.Text)
	a.label(150, 40, "Packager", ui.Accent)
	a.coloredButton(a.width-424, 18, 124, i18n.Tr("action.open_project"), color.RGBA{111, 48, 55, 255}, func() {
		a.choose("project")
	})
	a.coloredButton(a.width-290, 18, 128, i18n.Tr("action.open_settings"), color.RGBA{42, 77, 105, 255}, func() {
		a.choose("open")
	})
	a.coloredButton(a.width-152, 18, 128, i18n.Tr("action.save_settings"), color.RGBA{40, 88, 65, 255}, func() {
		a.choose("save")
	})
	a.renderSettings()
	a.fill(image.Rect(0, a.height-112, a.width, a.height), color.RGBA{24, 24, 24, 255})
	status := a.status
	for a.textWidth(status) > a.width-48 && len(status) > 0 {
		_, n := utf8.DecodeLastRuneInString(status)
		status = status[:len(status)-n]
	}
	a.label(24, a.height-82, status, ui.Muted)
	a.button(24, a.height-56, 146, i18n.Tr("action.open_turbowarp"), a.openTurboWarp)
	a.button(180, a.height-56, 146, i18n.Tr("action.connect_turbowarp"), func() {
		a.editorScreen = true
		a.editor = nil
		a.keyboardFocus = -1
	})
	a.button(a.width-190, a.height-56, 166, i18n.Tr("action.export"), func() {
		a.choose("export")
	})
	a.renderLanguage()
}

func (a *application) cancelJob() {
	if a.jobCancel != nil {
		a.jobCancel()
		a.status = i18n.Tr("job.canceling")
		a.dirty = true
	}
}

func (a *application) progressRect() image.Rectangle {
	x, y := (a.width-420)/2, (a.height-160)/2
	return image.Rect(x+24, y+65, x+396, y+73)
}

func (a *application) modalFrame(rect image.Rectangle) {
	draw.Draw(a.pixels, a.pixels.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 165}), image.Point{}, draw.Over)
	for spread := 10; spread >= 2; spread -= 2 {
		shadow := a.pixelRect(rect.Add(image.Pt(0, 5)).Inset(-spread))
		draw.Draw(a.pixels, shadow, image.NewUniform(color.RGBA{0, 0, 0, 22}), image.Point{}, draw.Over)
	}
	a.fill(rect, color.RGBA{112, 112, 112, 255})
	a.fill(rect.Inset(1), color.RGBA{38, 38, 38, 255})
	a.fill(image.Rect(rect.Min.X+1, rect.Min.Y+1, rect.Max.X-1, rect.Min.Y+4), ui.Accent)
}

func (a *application) renderBusy() {
	a.hits = nil
	x, y := (a.width-420)/2, (a.height-160)/2
	a.modalFrame(image.Rect(x, y, x+420, y+160))
	a.label(x+24, y+38, a.status, ui.Text)
	a.fill(a.progressRect(), ui.Background)
	a.button(x+152, y+102, 116, i18n.Tr("action.cancel_job"), a.cancelJob)
}

func displayChoice(s string) string {
	switch s {
	case "0":
		return i18n.Tr("choice.display_default")
	case "true":
		return i18n.Tr("choice.on")
	case "false":
		return i18n.Tr("choice.off")
	case "off":
		return i18n.Tr("choice.cloud_off")
	case "local":
		return i18n.Tr("choice.local")
	case "websocket":
		return i18n.Tr("choice.websocket")
	case "turbowarp":
		return "TurboWarp"
	case "custom":
		return i18n.Tr("choice.custom")
	case "inherit":
		return i18n.Tr("choice.inherit")
	case "preserve":
		return i18n.Tr("choice.preserve")
	case "stretch":
		return i18n.Tr("choice.stretch")
	case "integer":
		return i18n.Tr("choice.integer")
	}
	return s
}

func (a *application) renderBrowser() {
	b := a.browser
	a.hits = nil
	a.modalFrame(image.Rect(60, 70, a.width-60, a.height-70))
	a.label(82, 100, i18n.Tr("browser.title", b.mode), ui.Text)
	a.input(82, 114, a.width-360, &b.path)
	a.button(a.width-268, 114, 86, i18n.Tr("browser.go"), a.readDirectory)
	a.button(a.width-172, 114, 90, i18n.Tr("browser.up"), func() {
		b.path = filepath.Dir(b.path)
		a.readDirectory()
	})
	rows := max(1, (a.height-340)/30)
	b.scroll = min(b.scroll, max(0, len(b.entries)-rows))
	for row := 0; row < rows && row+b.scroll < len(b.entries); row++ {
		entry := b.entries[row+b.scroll]
		name := entry.Name()
		label := name
		if entry.IsDir() {
			label = i18n.Tr("browser.folder", name)
		}
		a.button(82, 160+row*30, a.width-164, label, func() {
			if entry.IsDir() {
				b.path = filepath.Join(b.path, name)
				b.name = ""
				a.readDirectory()
			} else {
				b.name = name
			}
		})
	}
	a.input(82, a.height-162, a.width-164, &b.name)
	a.button(82, a.height-116, 100, i18n.Tr("browser.select"), a.acceptFile)
	a.button(194, a.height-116, 100, i18n.Tr("action.cancel"), func() {
		a.browser = nil
		a.editor = nil
	})
	if host.HasDrives {
		a.button(308, a.height-116, 160, i18n.Tr("browser.drive"), func() {
			if path := host.NextDrive(b.path); path != "" {
				b.path = path
				a.readDirectory()
			}
		})
	}
}
