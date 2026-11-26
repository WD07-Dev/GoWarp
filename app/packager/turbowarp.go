package packager

import (
	"context"
	"fmt"
	"image"
	"runtime"
	"strings"
	"time"

	"gowarp/app/clipboard"
	"gowarp/app/config"
	"gowarp/app/editorbridge"
	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/project"

	webbrowser "github.com/pkg/browser"
)

type editorProjectRequest struct {
	ctx     context.Context
	server  *editorbridge.Server
	project *project.Project
	options editorbridge.Options
	done    chan error
}

func (a *application) previewMode() bool { return a.editorLocalForm != nil }

func (a *application) blockProjectChange() bool {
	if !a.previewMode() {
		return false
	}
	a.status = i18n.Tr("editor.disconnect_first")
	a.dirty = true
	return true
}

func (a *application) importEditorProject(r editorProjectRequest) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if a.busy || a.extensions != nil || a.previewMode() || a.editorBridge != r.server {
		return fmt.Errorf("packager is busy; close the current task before connecting")
	}
	settings := editorPreviewSettings("", r.project, r.options)
	if err := settings.Validate(); err != nil {
		return err
	}
	a.editorLocalForm, a.editorLocalTarget = a.form, a.target
	a.form = newForm(settings, variableNames(r.project))
	for i, template := range a.templates {
		if template.OS == runtime.GOOS && template.Arch == runtime.GOARCH {
			a.target = i
			break
		}
	}
	a.editorScreen = false
	a.browser, a.editor = nil, nil
	a.closeDropdown()
	a.keyboardFocus, a.scroll = -1, 0
	if a.section == 7 {
		a.section = 1
	}
	a.status, a.dirty = i18n.Tr("editor.preview_settings"), true
	a.syncEditorSettings()
	return nil
}

func (a *application) syncEditorSettings() {
	if a.editorBridge == nil || !a.previewMode() {
		return
	}
	s, err := a.form.settings()
	a.editorBridge.Configure(settingsForPlatform(s, runtime.GOOS), err)
}

func (a *application) leaveEditorPreview() {
	if !a.previewMode() {
		return
	}
	a.form, a.target = a.editorLocalForm, a.editorLocalTarget
	a.editorLocalForm = nil
	a.editor = nil
	a.closeDropdown()
	a.keyboardFocus, a.scroll = -1, 0
	a.status, a.dirty = i18n.Tr("editor.disconnected"), true
}

func (a *application) connectEditor() {
	if a.startEditorBridge() {
		a.copyEditorAddress()
	}
}

func (a *application) startEditorBridge() bool {
	if a.busy {
		return false
	}
	if a.editorBridge != nil {
		return true
	}
	if a.editorProjects == nil {
		a.editorProjects = make(chan editorProjectRequest)
	}
	for _, template := range a.templates {
		if template.OS != runtime.GOOS || template.Arch != runtime.GOARCH {
			continue
		}
		server, err := editorbridge.Start(a.ctx, func(ctx context.Context, path string, options editorbridge.Options) (<-chan error, error) {
			if options.Settings == nil {
				return nil, fmt.Errorf("connect the editor project first")
			}
			settings := *options.Settings
			settings.Project = path
			if options.ClickStart {
				settings.Player.AutoStart = false
			}
			if options.Display == "embedded" {
				settings.Player.Controls = false
			}
			preview, err := startPreview(ctx, ctx, template, path, settings, "GOWARP_EDITOR="+options.RuntimeURL, "GOWARP_EDITOR_DISPLAY="+options.Display)
			if err != nil {
				return nil, err
			}
			return preview.Done, nil
		})
		if err != nil {
			a.errorText = err.Error()
			a.dirty = true
			return false
		}
		a.editorBridge = server
		server.SetProjectHandler(func(ctx context.Context, p *project.Project, options editorbridge.Options) error {
			r := editorProjectRequest{ctx: ctx, server: server, project: p, options: options, done: make(chan error, 1)}
			select {
			case a.editorProjects <- r:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case err := <-r.done:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		return true
	}
	a.status = i18n.Tr("status.no_preview_runtime")
	a.dirty = true
	return false
}

func (a *application) openTurboWarp() {
	if a.busy {
		return
	}
	if a.editorBrowser == nil {
		server, err := editorbridge.Start(a.ctx, nil)
		if err != nil {
			a.errorText, a.dirty = err.Error(), true
			return
		}
		a.editorBrowser = server
	}
	server := a.editorBrowser
	a.job(i18n.Tr("editor.opening"), func(context.Context) result {
		address, err := server.TurboWarpURL()
		if err == nil {
			err = webbrowser.OpenURL(address)
		}
		return result{err: err, status: i18n.Tr("editor.opened")}
	})
}

func editorPreviewSettings(path string, p *project.Project, options editorbridge.Options) config.Settings {
	settings := config.DefaultForProject(path, p)
	settings.FPS = options.FPS
	if settings.FPS == 0 {
		settings.FPS = 60
	}
	settings.Width, settings.Height = options.Width, options.Height
	settings.Window.Width, settings.Window.Height = options.Width, options.Height
	settings.Turbo = options.Turbo
	settings.Interpolation = options.Interpolation
	if options.HighQualityPen != nil {
		settings.HighQualityPen = options.HighQualityPen
	}
	if options.RemoveFencing != nil {
		settings.RemoveFencing = options.RemoveFencing
	}
	if options.RemoveMiscLimits != nil {
		settings.RemoveMiscLimits = options.RemoveMiscLimits
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "TurboWarp"
	}
	settings.Player.Title = name
	settings.Player.Username = name
	settings.Player.AutoStart = !options.ClickStart
	return settings
}

func (a *application) copyEditorAddress() {
	if a.editorBridge == nil {
		return
	}
	address := a.editorBridge.URL()
	a.job(i18n.Tr("editor.copying"), func(ctx context.Context) result {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return result{err: clipboard.Write(ctx, address), status: i18n.Tr("editor.copied")}
	})
}

func (a *application) disconnectEditor() {
	if a.editorBridge == nil || a.busy {
		return
	}
	server := a.editorBridge
	a.job(i18n.Tr("editor.disconnecting"), func(context.Context) result {
		server.Close()
		return result{committed: true, closedEditor: server, status: i18n.Tr("editor.disconnected")}
	})
}

func (a *application) renderEditorConnection(left, right int) {
	a.label(left, 164, a.fitText(i18n.Tr("editor.instructions1"), right-left), ui.Text)
	a.label(left, 192, a.fitText(i18n.Tr("editor.instructions2"), right-left), ui.Muted)
	a.label(left, 220, a.fitText(i18n.Tr("editor.instructions3"), right-left), ui.Muted)
	a.label(left, 248, a.fitText(i18n.Tr("editor.instructions4"), right-left), ui.Muted)
	a.button(left, 272, 224, i18n.Tr("editor.connect"), a.connectEditor)
	if a.editorBridge == nil {
		return
	}
	a.button(left+236, 272, 180, i18n.Tr("editor.disconnect"), a.disconnectEditor)
	a.fill(image.Rect(left, 330, right, 370), ui.Field)
	a.label(left+8, 356, a.fitText(a.editorBridge.URL(), right-left-16), ui.Muted)
	status := a.editorBridge.Status()
	a.label(left, 400, i18n.Tr("editor.state."+status.Phase), ui.Text)
	if status.Error != "" {
		a.label(left, 430, a.fitText(status.Error, right-left), ui.Accent)
	}
}

func (a *application) renderTurboWarpScreen() {
	a.label(24, 42, i18n.Tr("action.connect_turbowarp"), ui.Text)
	a.button(a.width-150, 20, 120, i18n.Tr("extensions.back"), func() {
		a.editorScreen = false
		a.keyboardFocus = -1
	})
	a.label(24, 100, a.fitText(i18n.Tr("hint.turbowarp"), a.width-48), ui.Muted)
	a.renderEditorConnection(24, a.width-24)
	a.label(24, a.height-32, a.fitText(a.status, a.width-48), ui.Muted)
}
