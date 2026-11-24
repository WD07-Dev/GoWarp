package desktop

import (
	"encoding/json"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"gowarp/app/editorbridge"
	"gowarp/core/vm"
)

type editorInput struct {
	keys  map[string]bool
	x, y  float64
	down  bool
	frame *ebiten.Image
}

func (g *game) editorInput(in *Input) {
	peer := g.options.Editor
	if peer == nil {
		return
	}
	if peer.Embedded {
		*in = Input{MouseX: g.editor.x, MouseY: g.editor.y, MouseDown: g.editor.down}
	}
	for range 64 {
		select {
		case command := <-peer.Commands:
			reply := editorbridge.Reply{ID: command.ID}
			switch command.Action {
			case "edit":
				var edits []vm.EditorBlocks
				err := json.Unmarshal(command.Edits, &edits)
				if err == nil {
					err = g.controller.vm.UpdateEditorBlocks(edits)
				}
				if err != nil {
					reply.Error = err.Error()
				}
				peer.Reply(reply)
			case "flag":
				in.Restart = true
				peer.Reply(reply)
				return
			case "stop":
				in.Stop = true
				peer.Reply(reply)
				return
			case "toggle":
				running, err := g.controller.vm.ToggleScript(command.Target, command.Block, command.Reporter, func(value string, err error) {
					result := editorbridge.Reply{ID: command.ID, Reporter: true, Value: value}
					if err != nil {
						result.Error = err.Error()
					}
					peer.Reply(result)
				})
				if err != nil {
					reply.Error = err.Error()
				}
				reply.Running = running
				if err != nil || !command.Reporter || !running {
					peer.Reply(reply)
				}
			case "key":
				if !peer.Embedded || len(command.Key) > 64 {
					continue
				}
				if g.editor.keys == nil {
					g.editor.keys = make(map[string]bool)
				}
				if command.Down && !g.editor.keys[command.Key] {
					in.Pressed = append(in.Pressed, command.Key)
				} else if !command.Down && g.editor.keys[command.Key] {
					in.Released = append(in.Released, command.Key)
				}
				if command.Down {
					g.editor.keys[command.Key] = true
				} else {
					delete(g.editor.keys, command.Key)
				}
				in.Enter = in.Enter || command.Down && command.Key == "enter"
				in.Backspace = in.Backspace || command.Down && command.Key == "backspace"
			case "text":
				if peer.Embedded && len(in.Text)+len(command.Text) <= 4096 {
					in.Text += command.Text
				}
			case "mouse":
				if !peer.Embedded || math.IsNaN(command.X) || math.IsNaN(command.Y) {
					continue
				}
				g.editor.x, g.editor.y = g.inputPosition(int(command.X*float64(g.outsideWidth)), int(command.Y*float64(g.outsideHeight)))
				in.MousePressed = in.MousePressed || command.Down && !g.editor.down
				g.editor.down = command.Down
				in.MouseX, in.MouseY, in.MouseDown = g.editor.x, g.editor.y, g.editor.down
			case "release-input":
				for key := range g.editor.keys {
					in.Released = append(in.Released, key)
				}
				clear(g.editor.keys)
				g.editor.down, in.MouseDown = false, false
			}
		default:
			return
		}
	}
}

func (g *game) streamEditorFrame(screen *ebiten.Image) {
	peer := g.options.Editor
	if peer == nil || !peer.WantsFrame() {
		return
	}
	width, height := screen.Bounds().Dx(), screen.Bounds().Dy()
	w, h := peer.FrameSize(width, height)
	if w == width && h == height {
		if g.editor.frame != nil {
			g.editor.frame.Deallocate()
			g.editor.frame = nil
		}
		peer.Frame(w, h, screen.ReadPixels)
		return
	}
	if g.editor.frame == nil || g.editor.frame.Bounds().Dx() != w || g.editor.frame.Bounds().Dy() != h {
		if g.editor.frame != nil {
			g.editor.frame.Deallocate()
		}
		g.editor.frame = ebiten.NewImage(w, h)
	}
	op := &ebiten.DrawImageOptions{Blend: ebiten.BlendCopy, Filter: ebiten.FilterLinear}
	op.GeoM.Scale(float64(w)/float64(width), float64(h)/float64(height))
	g.editor.frame.DrawImage(screen, op)
	peer.Frame(w, h, g.editor.frame.ReadPixels)
}
