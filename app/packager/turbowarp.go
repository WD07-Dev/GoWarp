package packager

import (
	"context"
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
	for _, template := range a.templates {
		if template.OS != runtime.GOOS || template.Arch != runtime.GOARCH {
			continue
		}
		server, err := editorbridge.Start(a.ctx, func(ctx context.Context, path string, options editorbridge.Options) (<-chan error, error) {
			p, err := project.Load(path)
			if err != nil {
				return nil, err
			}
			settings := editorPreviewSettings(path, p, options)
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
		return true
	}
	a.status = i18n.Tr("status.no_preview_runtime")
	a.dirty = true
	return false
}

func (a *application) openTurboWarp() {
	if !a.startEditorBridge() {
		return
	}
	server := a.editorBridge
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
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "TurboWarp"
	}
	settings.Player.Title = name
	settings.Player.Username = name
	settings.Player.AutoStart = !options.ClickStart
	if options.Display == "embedded" {
		settings.Player.Controls = false
	}
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
