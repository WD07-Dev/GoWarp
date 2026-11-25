// Package window describes native viewports independently of the graphics backend.
package window

import (
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
)

var ErrClosed = errors.New("all native windows were closed")

/*
Desktop coordinates are physical pixels relative to the primary monitor.
Stage coordinates start at the stage's top-left, independently of window position.
*/
type View struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	X               int      `json:"x"`
	Y               int      `json:"y"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
	StageX          float64  `json:"stageX"`
	StageY          float64  `json:"stageY"`
	StageWidth      float64  `json:"stageWidth"`
	StageHeight     float64  `json:"stageHeight"`
	Border          bool     `json:"border"`
	Resizable       bool     `json:"resizable"`
	DisableMinimize bool     `json:"disableMinimize"`
	DisableMaximize bool     `json:"disableMaximize"`
	DisableClose    bool     `json:"disableClose"`
	Fullscreen      bool     `json:"fullscreen"`
	Topmost         bool     `json:"topmost"`
	Passthrough     bool     `json:"passthrough"`
	Hidden          bool     `json:"hidden"`
	Opacity         *float64 `json:"opacity,omitempty"`
}

type Layout struct {
	Views []View
	Tiles []image.Rectangle
	Size  image.Point
}

func Pack(views []View) (Layout, error) {
	l := Layout{Views: append([]View(nil), views...), Size: image.Pt(1, 1)}
	if len(views) > 16 {
		return l, fmt.Errorf("at most 16 native windows are supported")
	}
	ids := make(map[string]bool, len(views))
	x, y, row := 0, 0, 0
	for _, v := range views {
		if v.ID == "" || len(v.ID) > 128 || ids[v.ID] || strings.ContainsRune(v.ID, 0) || strings.ContainsRune(v.Title, 0) {
			return l, fmt.Errorf("invalid or duplicate native window ID/title %q", v.ID)
		}
		ids[v.ID] = true
		if v.Width < 1 || v.Height < 1 || v.Width > 4096 || v.Height > 4096 {
			return l, fmt.Errorf("window %q dimensions must be 1..4096", v.ID)
		}
		alpha := v.Alpha()
		for _, n := range []float64{v.StageX, v.StageY, v.StageWidth, v.StageHeight, alpha} {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return l, fmt.Errorf("non-finite window value")
			}
		}
		validViewport := v.StageWidth > 0 && v.StageWidth <= 16384 && v.StageHeight > 0 && v.StageHeight <= 16384
		validPosition := v.X >= -100000 && v.X <= 100000 && v.Y >= -100000 && v.Y <= 100000
		if !validViewport || !validPosition || alpha < 0 || alpha > 1 {
			return l, fmt.Errorf("invalid window %q viewport, position or opacity", v.ID)
		}
		if x+v.Width > 4096 {
			x = 0
			y += row
			row = 0
		}
		if y+v.Height > 4096 {
			return l, fmt.Errorf("native window atlas exceeds 4096x4096")
		}
		l.Tiles = append(l.Tiles, image.Rect(x, y, x+v.Width, y+v.Height))
		l.Size.X = max(l.Size.X, x+v.Width)
		l.Size.Y = max(l.Size.Y, y+v.Height)
		x += v.Width
		row = max(row, v.Height)
	}
	return l, nil
}

func (v View) Alpha() float64 {
	if v.Opacity == nil {
		return 1
	}
	return *v.Opacity
}

func (v View) StagePoint(x, y float64) (float64, float64) {
	return v.StageX + x*v.StageWidth/float64(v.Width), v.StageY + y*v.StageHeight/float64(v.Height)
}

type Input struct {
	Window                          string
	X, Y                            float64
	Focused, Visible, Down, Pressed bool
	Keys, Released                  []string
	Text                            string
	Wheel                           float64
}

type Backend interface {
	Prepare() error
	SetViews([]View) error
	Publish(bool)
	Poll() (Input, error)
	Views() []View
	Close() error
}
