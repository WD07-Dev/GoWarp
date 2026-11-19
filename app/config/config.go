// Package config defines the settings shared by the packager and player.
package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gowarp/app/assets"
	"gowarp/core/project"
)

const Version = 1

type Window struct {
	// Initial game display size, excluding player controls. Zero inherits the stage size.
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Resizable       bool   `json:"resizable"`
	Fullscreen      bool   `json:"fullscreen"`
	Fit             string `json:"fit"`
	Background      string `json:"background"`
	DisableMinimize bool   `json:"disableMinimize"`
	DisableMaximize bool   `json:"disableMaximize"`
}

type Canvas struct {
	Antialias bool `json:"antialias"`
}

type Player struct {
	RenderFPS   int    `json:"renderFPS"`
	Icon        []byte `json:"icon,omitempty"`
	Title       string `json:"title"`
	AutoStart   bool   `json:"autoStart"`
	CloseOnStop bool   `json:"closeOnStop"`
	Controls    bool   `json:"controls"`
	Mute        bool   `json:"mute"`
	Username    string `json:"username"`
	Accent      string `json:"accent"`
}

type Cloud struct {
	Mode      string            `json:"mode"`
	Server    string            `json:"server"`
	ProjectID string            `json:"projectId"`
	Username  string            `json:"username"`
	Contact   string            `json:"contact"`
	Variables map[string]string `json:"variables,omitempty"`
}

type Settings struct {
	Version       int     `json:"version"`
	AppID         string  `json:"appId"`
	Project       string  `json:"project,omitempty"`
	FPS           float64 `json:"fps"`
	Turbo         bool    `json:"turbo"`
	Interpolation bool    `json:"interpolation"`
	// Missing optional values retain the project settings in older packages.
	RemoveFencing    *bool           `json:"removeFencing,omitempty"`
	RemoveMiscLimits *bool           `json:"removeMiscLimits,omitempty"`
	HighQualityPen   *bool           `json:"highQualityPen,omitempty"`
	Width            int             `json:"width"`
	Height           int             `json:"height"`
	Window           Window          `json:"window"`
	Canvas           Canvas          `json:"canvas"`
	Player           Player          `json:"player"`
	Cloud            Cloud           `json:"cloud"`
	Loading          LoadingSettings `json:"loading"`
}

func Default(p *project.Project) Settings {
	highQualityPen, removeFencing, removeMiscLimits := false, false, false
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	s := Settings{
		HighQualityPen:   &highQualityPen,
		RemoveFencing:    &removeFencing,
		RemoveMiscLimits: &removeMiscLimits,
		Version:          Version,
		AppID:            hex.EncodeToString(id[:]),
		FPS:              30,
		Width:            480,
		Height:           360,
		Window:           Window{Resizable: true, Fit: "preserve", Background: "#1f1f1f"},
		Loading:          DefaultLoadingSettings(),
		Player: Player{
			Title:     "GoWarp",
			Username:  "GoWarp",
			AutoStart: true,
			Controls:  true,
			Accent:    "#ff4b51",
		},
		Cloud: Cloud{
			Mode:     "off",
			Server:   "wss://clouddata.turbowarp.org",
			Username: "GoWarp",
		},
	}
	applyProjectSettings(&s, p)
	s.Window.Width = s.Width
	s.Window.Height = s.Height
	s.Canvas = Canvas{Antialias: true}
	return s
}

// DefaultForProject uses the project name for the initial title and usernames.
func DefaultForProject(path string, p *project.Project) Settings {
	s := Default(p)
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s.Project = path
	s.Player.Title = name
	s.Player.Username = name
	s.Cloud.Username = name
	return s
}

// DisplaySize returns the initial visible game area without changing stage coordinates.
func (s Settings) DisplaySize() (int, int) {
	width, height := s.Window.Width, s.Window.Height
	if width == 0 {
		width = s.Width
	}
	if height == 0 {
		height = s.Height
	}
	return width, height
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

var hexColor = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

func (s Settings) Validate() error {
	if s.Player.RenderFPS != 0 && s.Player.RenderFPS != 30 && s.Player.RenderFPS != 60 {
		return fmt.Errorf("display FPS must be 0, 30 or 60")
	}
	if err := s.Loading.Validate(); err != nil {
		return err
	}
	if _, err := assets.DecodeIcon(s.Player.Icon); err != nil {
		return fmt.Errorf("app icon: %w", err)
	}
	if s.Version != Version {
		return fmt.Errorf("unsupported settings version: %d", s.Version)
	}
	if !safeID.MatchString(s.AppID) {
		return fmt.Errorf("invalid application ID")
	}
	if math.IsNaN(s.FPS) || math.IsInf(s.FPS, 0) || s.FPS <= 0 || s.FPS > 1000 {
		return fmt.Errorf("FPS must be greater than 0 and at most 1000")
	}
	if s.Width < 1 || s.Height < 1 || s.Width > 4096 || s.Height > 4096 {
		return fmt.Errorf("stage dimensions must be 1..4096")
	}
	if s.Window.Width < 0 || s.Window.Height < 0 || s.Window.Width > 4096 || s.Window.Height > 4096 {
		return fmt.Errorf("game display dimensions must be 1..4096, or 0 to use stage size")
	}
	if s.Window.Fit != "preserve" && s.Window.Fit != "stretch" && s.Window.Fit != "integer" {
		return fmt.Errorf("invalid window fit")
	}
	if !hexColor.MatchString(s.Window.Background) || (s.Player.Controls && !hexColor.MatchString(s.Player.Accent)) {
		return fmt.Errorf("colors must be #RRGGBB")
	}
	network := s.Cloud.Mode == "websocket"
	validMode := func(m string) bool {
		return m == "off" || m == "local" || m == "websocket"
	}
	if !validMode(s.Cloud.Mode) {
		return fmt.Errorf("invalid cloud mode")
	}
	for _, m := range s.Cloud.Variables {
		if !validMode(m) {
			return fmt.Errorf("invalid variable cloud mode")
		}
		network = network || m == "websocket"
	}
	if network {
		u, err := url.Parse(s.Cloud.Server)
		if err != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") || u.User != nil {
			return fmt.Errorf("cloud server must be a ws:// or wss:// URL")
		}
		if strings.TrimSpace(s.Cloud.ProjectID) == "" || strings.TrimSpace(s.Cloud.Contact) == "" {
			return fmt.Errorf("cloud project ID and contact are required")
		}
	}
	return nil
}

func Load(path string) (Settings, error) {
	s := Default(nil)
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSettingsSize+1))
	if err != nil {
		return s, err
	}
	if len(data) > MaxSettingsSize {
		return s, fmt.Errorf("settings file too large")
	}
	var header struct {
		Project string
	}
	if err = json.Unmarshal(data, &header); err != nil {
		return s, err
	}
	if header.Project != "" {
		if !filepath.IsAbs(header.Project) {
			header.Project = filepath.Join(filepath.Dir(path), header.Project)
		}
		p, err := project.Load(header.Project)
		if err != nil {
			return s, err
		}
		s = DefaultForProject(header.Project, p)
	}
	s.Window.Width, s.Window.Height = 0, 0
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&s); err != nil {
		return s, err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return s, fmt.Errorf("unexpected settings data")
	}
	if s.Project != "" && !filepath.IsAbs(s.Project) {
		s.Project = filepath.Join(filepath.Dir(path), s.Project)
	}
	s.Window.Width, s.Window.Height = s.DisplaySize()
	return s, s.Validate()
}

func Save(path string, s Settings) error {
	return SaveContext(context.Background(), path, s)
}

func SaveContext(ctx context.Context, path string, s Settings) error {
	s.Window.Width, s.Window.Height = s.DisplaySize()
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > MaxSettingsSize {
		return fmt.Errorf("settings file too large")
	}
	return writeAtomic(ctx, path, append(data, '\n'))
}

func WriteAtomic(path string, data []byte) error {
	return writeAtomic(context.Background(), path, data)
}

func writeAtomic(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gowarp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
