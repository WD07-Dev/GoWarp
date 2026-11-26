package editorbridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gowarp/app/config"
	"gowarp/core/project"
)

func TestRunUsesPackagerSettingsSnapshot(t *testing.T) {
	launches := make(chan Options, 2)
	s, err := Start(context.Background(), func(ctx context.Context, _ string, options Options) (<-chan error, error) {
		launches <- options
		done := make(chan error, 1)
		go func() { <-ctx.Done(); done <- nil }()
		return done, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	settings := config.Default(nil)
	settings.FPS = 120
	settings.Platform.MultiWindow = true
	s.Configure(settings, nil)
	revision := s.Status().SettingsRevision
	s.Configure(settings, nil)
	if s.Status().SettingsRevision != revision {
		t.Fatal("unchanged settings changed revision")
	}
	*settings.HighQualityPen = true
	run := func() Options {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://"+s.address+"/run", strings.NewReader("archive"))
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("X-GoWarp-Options", `{"name":"Preview","fps":30,"width":480,"height":360,"display":"embedded"}`)
		res := httptest.NewRecorder()
		s.ServeHTTP(res, req)
		if res.Code != http.StatusAccepted {
			t.Fatal(res.Code, res.Body.String())
		}
		select {
		case o := <-launches:
			return o
		case <-time.After(3 * time.Second):
			t.Fatal("launch did not run")
			return Options{}
		}
	}
	first := run()
	if first.Settings.FPS != 120 || *first.Settings.HighQualityPen || first.Display != "native" {
		t.Fatalf("settings snapshot or native constraint lost: %+v", first)
	}
	settings.FPS = 60
	settings.Platform.MultiWindow = false
	s.Configure(settings, nil)
	if first.Settings.FPS != 120 || *first.Settings.HighQualityPen {
		t.Fatal("running settings were mutated")
	}
	if s.Status().SettingsRevision == revision {
		t.Fatal("changed settings kept revision")
	}
	s.Stop()
	waitPhase(t, s, "ready")
	second := run()
	if second.Settings.FPS != 60 || !*second.Settings.HighQualityPen || second.Display != "embedded" {
		t.Fatal("next run did not use edited settings")
	}
	s.Stop()
	waitPhase(t, s, "ready")
	s.Configure(settings, errors.New("invalid FPS"))
	res := request(t, s, "POST", "/run", "https://turbowarp.org", s.token, strings.NewReader("archive"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid settings launched a game")
	}
}

func TestDisconnectCancelsPendingConnection(t *testing.T) {
	s, err := Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	entered := make(chan struct{})
	s.SetProjectHandler(func(ctx context.Context, p *project.Project, options Options) error {
		if len(p.Targets) != 1 || options.FPS != 60 {
			return errors.New("project metadata missing")
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	req := httptest.NewRequest(http.MethodPost, "http://"+s.address+"/connect", strings.NewReader(`{"targets":[{"isStage":true,"name":"Stage"}]}`))
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("X-GoWarp-Options", `{"fps":60,"width":480,"height":360}`)
	res := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.ServeHTTP(res, req); close(done) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
	request(t, s, "POST", "/disconnect", "https://turbowarp.org", s.token, nil)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not cancel connection")
	}
	if s.Status().Connected || s.Status().Connecting || res.Code == http.StatusOK {
		t.Fatal("canceled connection became active")
	}
}
