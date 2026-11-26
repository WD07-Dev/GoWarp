// Package player runs a packaged project without the packager UI.
package player

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gowarp/app/compiler"
	"gowarp/app/config"
	"gowarp/app/desktop"
	"gowarp/app/editorbridge"
	"gowarp/app/host"
	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/cloud"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func Main() {
	os.Exit(runMainWithExtensions(nil))
}

func MainWithExtensions(bundle extension.Bundle) {
	os.Exit(runMainWithExtensions(bundle))
}

func readPlayerProject(executable string) (*compiler.Package, error) {
	if os.Getenv("GOWARP_PREVIEW") == "1" {
		dir := os.Getenv("GOWARP_WORKDIR")
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("preview directory must be an absolute path")
		}
		return compiler.ReadPreview(dir)
	}
	return compiler.ReadPackage(executable)
}

func runMainWithExtensions(bundle extension.Bundle) int {
	path, err := os.Executable()
	var p *compiler.Package
	if err == nil {
		p, err = readPlayerProject(path)
	}
	appID := "gowarp"
	if p != nil {
		appID = p.Settings.AppID
	}
	diagnostics, logErr := startRuntimeDiagnostics(os.Args[1:], appID)
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "runtime diagnostics:", logErr)
	}
	/*
		Do not defer normal completion: an unhandled panic must leave the
		crash descriptor installed, including for panics in other goroutines.
	*/
	code := 1
	if err != nil {
		diagnostics.record("package error: " + err.Error())
		fmt.Fprintln(os.Stderr, err)
		ui.ShowMessage("GoWarp", err.Error())
	} else {
		code = runPackaged(p, bundle, diagnostics)
	}
	diagnostics.finish(code)
	return code
}

func runPackaged(p *compiler.Package, bundle extension.Bundle, diagnostics *runtimeLog) int {
	if priority, err := host.ConfigureGamePriority(); err != nil {
		diagnostics.record(fmt.Sprintf("process_priority error=%q", err))
	} else if priority != "" {
		diagnostics.record(priority)
	}
	diagnostics.record(fmt.Sprintf("package name=%q app_id=%q", p.Name, p.Settings.AppID))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if diagnostics != nil {
		ctx = context.WithValue(ctx, runtimeLogKey{}, diagnostics)
	}
	if stop := os.Getenv("GOWARP_STOP_FILE"); stop != "" && os.Getenv("GOWARP_PREVIEW") == "1" {
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := os.Stat(stop); err == nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	preview := os.Getenv("GOWARP_PREVIEW") == "1"
	path := p.Settings.Project
	var extract func(context.Context) error
	if !preview {
		dir, err := os.MkdirTemp("", "gowarp-player-*")
		if err != nil {
			ui.ShowMessage("GoWarp", err.Error())
			return 1
		}
		defer os.RemoveAll(dir)
		path = filepath.Join(dir, p.Name)
		extract = func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			err := os.WriteFile(path, p.Project, 0600)
			p.Project = nil
			return err
		}
	}
	started := false
	if err := runPrepared(ctx, path, p.Settings, preview, &started, extract, bundle); err != nil && ctx.Err() == nil {
		diagnostics.record("runtime error: " + err.Error())
		fmt.Fprintln(os.Stderr, err)
		if !started {
			ui.ShowMessage(i18n.Tr("player.error_title"), err.Error())
		}
		return 1
	}
	return 0
}

func Run(ctx context.Context, path string, s config.Settings, preview bool) error {
	return runPrepared(ctx, path, s, preview, nil, nil)
}

func runPrepared(ctx context.Context, path string, s config.Settings, preview bool, started *bool, prepare func(context.Context) error, bundles ...extension.Bundle) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var service *cloud.Session
	defer func() {
		if service != nil {
			service.Close()
		}
	}()
	previous := map[string]string{}
	options := desktop.Options{
		Settings: &s,
		FPS:      s.FPS,
		Turbo:    s.Turbo,
		Mute:     s.Player.Mute,
	}
	if address := os.Getenv("GOWARP_EDITOR"); preview && address != "" {
		peer, err := editorbridge.ConnectRuntime(ctx, address, os.Getenv("GOWARP_EDITOR_DISPLAY") == "embedded")
		if err != nil {
			return err
		}
		defer peer.Close()
		options.Editor = peer
	}
	if diagnostics, ok := ctx.Value(runtimeLogKey{}).(*runtimeLog); ok {
		options.Diagnostic = diagnostics.record
	}
	if len(bundles) > 0 {
		options.Extensions = bundles[0]
	}
	options.Prepare = func(loadCtx context.Context) (*project.Project, error) {
		if prepare != nil {
			if err := prepare(loadCtx); err != nil {
				return nil, err
			}
		}
		p, err := project.Load(path)
		if err != nil {
			return nil, err
		}
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(root, "GoWarp", s.AppID)
		if preview {
			root = filepath.Join(root, "preview")
		}
		service, err = cloud.Open(loadCtx, cloudOptions(s, p), root)
		return p, err
	}
	// These callbacks are first used after Prepare's result reaches the window.
	options.Status = func() string {
		return service.Status()
	}
	options.OnStart = func() {
		if started != nil {
			*started = true
		}
	}
	options.BeforeTick = func(r *vm.Runtime) error {
		for id, value := range service.Poll() {
			r.ApplyCloud(id, value)
		}
		for id, v := range r.CloudValues() {
			previous[id] = cloudText(v.Value)
		}
		return nil
	}
	options.AfterTick = func(r *vm.Runtime) error {
		for id, v := range r.CloudValues() {
			value := cloudText(v.Value)
			if previous[id] != value {
				service.Set(id, value)
			}
		}
		return nil
	}
	_, err := desktop.Run(ctx, path, nil, options, nil)
	return err
}

func cloudOptions(s config.Settings, p *project.Project) cloud.Options {
	o := cloud.Options{
		Mode:      "off",
		Modes:     map[string]string{},
		Names:     map[string]string{},
		Server:    s.Cloud.Server,
		ProjectID: s.Cloud.ProjectID,
		Username:  s.Player.Username,
		Contact:   s.Cloud.Contact,
	}
	switch s.Cloud.Provider {
	case "turbowarp":
		o.Server = "wss://clouddata.turbowarp.org"
	case "custom":
		o.Contact = ""
		if strings.TrimSpace(o.ProjectID) == "" {
			o.ProjectID = s.AppID
		}
	}
	for id, v := range vm.New(p).CloudValues() {
		o.Names[id] = v.Name
		mode := s.Cloud.Mode
		if m, ok := s.Cloud.Variables[id]; ok {
			mode = m
		}
		o.Modes[id] = mode
	}
	return o
}

func cloudText(value any) string {
	if n, ok := value.(float64); ok {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}
