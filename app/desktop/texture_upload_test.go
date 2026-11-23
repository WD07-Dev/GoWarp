package desktop

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

func TestUniformTextureDoesNotEvictOtherCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 512))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		var first *ebiten.Image
		for i := 0; i < 31; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("working:%d", i), Image: pixels}
			texture, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if i == 0 {
				first = texture
			}
		}
		background := image.NewNRGBA(image.Rect(0, 0, 2560, 1920))
		draw.Draw(background, background.Bounds(), image.NewUniform(color.NRGBA{A: 255}), image.Point{}, draw.Src)
		gpu.measureUploads = true
		texture, temporary := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{Image: background}, Density: 1})
		if temporary || texture == nil || texture.Bounds().Size() != image.Pt(1, 1) {
			t.Fatal("uniform background retained a full-size GPU texture")
		}
		cached, _ := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "working:0", Image: pixels}, Density: 1})
		if cached != first || gpu.evictions != 0 || gpu.uploadBytes != 4 || gpu.bytes > stageTextureBudget {
			t.Fatalf("uniform background displaced working textures: evictions=%d upload=%d cache=%d", gpu.evictions, gpu.uploadBytes, gpu.bytes)
		}
	})
}

func TestSceneBitmapPreparationSkipsUploadedAndInvisibleCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		empty := project.Costume{MD5Ext: "empty.svg", DataFormat: "svg"}
		if err := os.WriteFile(filepath.Join(dir, empty.MD5Ext), []byte(`<svg width="1" height="1"></svg>`), 0600); err != nil {
			t.Fatal(err)
		}
		target := &project.Target{Costumes: []project.Costume{empty}}
		for i := 1; i <= 5; i++ {
			pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
			pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i), A: 255})
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, pixels); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("image%d.png", i)
			if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
		}
		project := &project.Project{Targets: []*project.Target{target}}
		lib, err := media.LoadRuntime(context.Background(), dir, project, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		if err := lib.EnableDisplayPreparation(dir, project); err != nil {
			t.Fatal(err)
		}
		r := render.New(lib)
		gpu := stageGPU{cacheSize: image.Pt(480, 360)}
		defer gpu.Close()
		for i := 1; i <= 4; i++ {
			gpu.upcomingScene = append(gpu.upcomingScene, vm.DisplayTarget{State: vm.TargetState{
				Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, CurrentCostume: i,
			}})
		}
		gpu.upcomingScene[2].State.Visible = false
		gpu.upcomingScene[3].State.X = 10000
		// Model a GPU-resident image whose CPU pixels have been evicted.
		identity, _ := lib.CostumeImageCacheKey(lib.Costumes[0][2], 1)
		gpu.textures = map[stageTextureKey]stageTexture{
			{library: lib, image: identity}: {image: ebiten.NewImage(32, 32)},
		}
		// Repeated clones must not consume the entire image lookahead window.
		clones := make([]vm.DisplayTarget, 32)
		for i := range clones {
			clones[i] = gpu.upcomingScene[1]
			clones[i].State.CloneID = i + 1
		}
		gpu.upcomingScene = append(clones, gpu.upcomingScene...)
		gpu.prepareSceneBitmaps(r, "")
		if lib.PrepareDisplayBitmap(lib.Costumes[0][1]) {
			t.Fatal("visible upcoming image was not already being prepared")
		}
		// A second miss in the same pen replay must not rescan the scene.
		gpu.upcomingScene[0].State.CurrentCostume = 5
		gpu.prepareSceneBitmaps(r, "")
		if !lib.PrepareDisplayBitmap(lib.Costumes[0][5]) {
			t.Fatal("same scene cursor repeated its preparation scan")
		}
		for _, i := range []int{2, 3} {
			if !lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
				t.Fatalf("uploaded/hidden image %d was prepared unnecessarily", i)
			}
		}
	})
}

func TestStampBitmapPreparationIncludesHiddenSprites(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "empty.svg"), []byte(`<svg width="1" height="1"></svg>`), 0600); err != nil {
			t.Fatal(err)
		}
		target := &project.Target{Costumes: []project.Costume{{MD5Ext: "empty.svg", DataFormat: "svg"}}}
		for i := 1; i <= 3; i++ {
			pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
			pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i), A: 255})
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, pixels); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("stamp%d.png", i)
			if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
		}
		project := &project.Project{Targets: []*project.Target{target}}
		lib, err := media.LoadRuntime(context.Background(), dir, project, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		if err := lib.EnableDisplayPreparation(dir, project); err != nil {
			t.Fatal(err)
		}
		r := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= 3; i++ {
			pen.replaying = append(pen.replaying, recordedPen{command: vm.PenCommand{
				Kind: "stamp", Target: vm.DisplayTarget{State: vm.TargetState{
					Size: 100, StretchX: 100, StretchY: 100, Direction: 90, CurrentCostume: i,
				}},
			}})
		}
		pen.replaying[3].command.Target.State.X = 10000
		// The second upcoming stamp already has GPU pixels, but no CPU copy.
		identity, _ := lib.CostumeImageCacheKey(lib.Costumes[0][2], 1)
		gpu.textures = map[stageTextureKey]stageTexture{
			{library: lib, image: identity}: {image: ebiten.NewImage(32, 32)},
		}
		clones := make([]recordedPen, 32)
		for i := range clones {
			clones[i] = pen.replaying[2]
			clones[i].command.Target.State.CloneID = i + 1
		}
		pen.replaying = append(pen.replaying[:1:1], append(clones, pen.replaying[1:]...)...)
		gpu.texture(r, render.DisplaySprite{Costume: lib.Costumes[0][0], Density: 1})
		if lib.PrepareDisplayBitmap(lib.Costumes[0][1]) {
			t.Error("hidden upcoming stamp was not prepared during the current upload")
		}
		for _, i := range []int{2, 3} {
			if !lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
				t.Errorf("uploaded/offscreen stamp %d was prepared unnecessarily", i)
			}
		}
	})
}

func TestBitmapPreparationPrioritizesNextImages(t *testing.T) {
	runGPUFrame(t, func() {
		for _, stamp := range []bool{false, true} {
			t.Run(fmt.Sprintf("stamp=%v", stamp), func(t *testing.T) {
				dir := t.TempDir()
				target := &project.Target{}
				for i := 0; i < 10; i++ {
					pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
					pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i), A: 255})
					var encoded bytes.Buffer
					if err := png.Encode(&encoded, pixels); err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("next%d.png", i)
					if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
				}
				project := &project.Project{Targets: []*project.Target{target}}
				lib, err := media.LoadRuntime(context.Background(), dir, project, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer lib.Close()
				if err := lib.EnableDisplayPreparation(dir, project); err != nil {
					t.Fatal(err)
				}
				r := render.New(lib)
				gpu := stageGPU{cacheSize: image.Pt(480, 360)}
				defer gpu.Close()
				for i := 1; i <= 8; i++ {
					gpu.upcomingScene = append(gpu.upcomingScene, vm.DisplayTarget{State: vm.TargetState{CurrentCostume: i, Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}})
				}
				if stamp {
					pen, err := gpu.enablePen(r)
					if err != nil {
						t.Fatal(err)
					}
					pen.replaying = append(pen.replaying, recordedPen{})
					for _, target := range gpu.upcomingScene {
						pen.replaying = append(pen.replaying, recordedPen{command: vm.PenCommand{Kind: "stamp", Target: target}})
					}
					pen.prepareStampBitmaps("")
					pen.replaying = nil
				} else {
					gpu.prepareSceneBitmaps(r, "")
				}
				for i := 1; i <= 4; i++ {
					if lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
						t.Fatalf("upcoming image %d was not already prepared", i)
					}
				}
				for i := 1; i <= 4; i++ {
					if pixels := lib.CostumeImage(lib.Costumes[0][i]); pixels == nil || pixels.NRGBAAt(0, 0).R != uint8(i) {
						t.Fatal("missing upcoming pixels", lib.Err())
					}
				}
				/*
					Later candidates must still be new work; only the nearest
					four distinct images belong in the initial batch.
				*/
				for i := 5; i <= 8; i++ {
					deadline := time.Now().Add(2 * time.Second)
					for {
						lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
						if lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
							break
						}
						if time.Now().After(deadline) {
							t.Fatalf("image %d occupied preparation slots before the next four", i)
						}
						time.Sleep(time.Millisecond)
					}
				}
			})
		}
	})
}

func TestDeferredStampPreparesBitmapBeforeDraw(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "empty.svg"), []byte(`<svg width="1" height="1"></svg>`), 0600); err != nil {
			t.Fatal(err)
		}
		costumes := []project.Costume{{MD5Ext: "empty.svg", DataFormat: "svg"}}
		for i := 1; i <= 5; i++ {
			pixels := image.NewNRGBA(image.Rect(0, 0, 32, 32))
			pixels.SetNRGBA(0, 0, color.NRGBA{R: uint8(i), A: 255})
			var data bytes.Buffer
			if err := png.Encode(&data, pixels); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("early%d.png", i)
			if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			costumes = append(costumes, project.Costume{MD5Ext: name, DataFormat: "png"})
		}
		project := &project.Project{Targets: []*project.Target{{Costumes: costumes}}}
		lib, err := media.LoadRuntime(context.Background(), dir, project, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		if err := lib.EnableDisplayPreparation(dir, project); err != nil {
			t.Fatal(err)
		}
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(render.New(lib))
		if err != nil {
			t.Fatal(err)
		}
		// The full GPU image can survive eviction of its CPU pixels.
		identity, _ := lib.CostumeImageCacheKey(lib.Costumes[0][5], 1)
		gpu.textures = map[stageTextureKey]stageTexture{
			{library: lib, image: identity}: {image: ebiten.NewImage(32, 32)},
		}
		recorder := deferredPen{pen}
		for i := 1; i <= 5; i++ {
			state := vm.TargetState{Size: 100, StretchX: 100, StretchY: 100, Direction: 90, CurrentCostume: i}
			switch i {
			case 2:
				state.X = 10000
			case 3:
				state.Effects.Ghost = 100
			case 4:
				state.MaskMode = true
				state.Name = "masked"
			}
			recorder.Pen(vm.PenCommand{Kind: "stamp", Target: vm.DisplayTarget{State: state}})
		}
		if len(gpu.textures) != 1 || pen.image != nil {
			t.Fatal("recording uploaded pixels before drawing")
		}
		if lib.PrepareDisplayBitmap(lib.Costumes[0][1]) {
			t.Fatal("recorded stamp waited until drawing to start decoding")
		}
		for i := 2; i <= 5; i++ {
			deadline := time.Now().Add(2 * time.Second)
			for !lib.PrepareDisplayBitmap(lib.Costumes[0][i]) {
				if time.Now().After(deadline) {
					t.Fatalf("invisible or GPU-cached stamp %d was decoded unnecessarily", i)
				}
				lib.PrepareDisplayCostume(lib.Costumes[0][0], nil)
				time.Sleep(time.Millisecond)
			}
			// Host-claimed jobs release their reservation after worker handoff.
			if lib.CostumeImage(lib.Costumes[0][i]) == nil {
				t.Fatal(lib.Err())
			}
		}
		pixels := lib.CostumeImage(lib.Costumes[0][1])
		if pixels == nil || pixels.NRGBAAt(0, 0) != (color.NRGBA{R: 1, A: 255}) {
			t.Fatalf("prepared stamp pixels changed: %v", lib.Err())
		}
	})
}

func TestUniformTexturePreservesEffects(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		if err := gpu.initShader(); err != nil {
			t.Fatal(err)
		}
		stampShader, err := ebiten.NewShader(stampShaderSource)
		if err != nil {
			t.Fatal(err)
		}
		defer stampShader.Deallocate()
		corners := [4][2]float64{{3.25, 4.5}, {43.25, 9.5}, {38.25, 33.5}, {-1.75, 28.5}}
		for _, alpha := range []uint8{0, 80, 255} {
			runtime.GC()
			pixels := image.NewNRGBA(image.Rect(0, 0, 12, 8))
			draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 40, G: 100, B: 180, A: alpha}), image.Point{}, draw.Src)
			costume := &media.Costume{CacheKey: fmt.Sprintf("uniform-alpha:%d", alpha), Image: pixels, Width: 12, Height: 8}
			compact, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			full := ebiten.NewImage(12, 8)
			stageUpload(full, pixels)
			for _, nearest := range []bool{false, true} {
				for _, effects := range []vm.GraphicEffects{{}, {Color: 78, Brightness: -20, Ghost: 37}, {Whirl: 70, Fisheye: -30, Mosaic: 30, Pixelate: 7}} {
					got, want := ebiten.NewImage(48, 40), ebiten.NewImage(48, 40)
					got.Fill(color.RGBA{R: 30, G: 60, B: 90, A: 255})
					want.Fill(color.RGBA{R: 30, G: 60, B: 90, A: 255})
					gpu.quad(got, compact, corners, 12, 8, effects, nearest, false)
					gpu.quad(want, full, corners, 12, 8, effects, nearest, false)
					a, b := make([]byte, 48*40*4), make([]byte, 48*40*4)
					got.ReadPixels(a)
					want.ReadPixels(b)
					got.Deallocate()
					want.Deallocate()
					if !bytes.Equal(a, b) {
						for i := range a {
							if a[i] != b[i] {
								t.Fatalf("compact texture changed pixels: alpha=%d nearest=%v effects=%+v pixel=(%d,%d) channel=%d got=%d want=%d", alpha, nearest, effects, (i/4)%48, i/(4*48), i%4, a[i], b[i])
							}
						}
					}
					if effects.Whirl == 0 && effects.Fisheye == 0 && effects.Mosaic == 0 && effects.Pixelate == 0 {
						got, want = ebiten.NewImage(48, 40), ebiten.NewImage(48, 40)
						pen := penGPU{image: got, stampShader: stampShader}
						pen.stamp(compact, corners, effects, nearest)
						pen.flushStamps()
						pen.image = want
						pen.stamp(full, corners, effects, nearest)
						pen.flushStamps()
						got.ReadPixels(a)
						want.ReadPixels(b)
						got.Deallocate()
						want.Deallocate()
						if !bytes.Equal(a, b) {
							t.Fatalf("compact pen stamp changed pixels: alpha=%d nearest=%v effects=%+v", alpha, nearest, effects)
						}
					}
				}
			}
			full.Deallocate()
		}
	})
}

func TestCompactTexturePixelsChecksEntireRegion(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 12, 10))
	region := pixels.SubImage(image.Rect(2, 3, 8, 9)).(*image.NRGBA)
	fill := color.NRGBA{R: 20, G: 80, B: 160, A: 127}
	draw.Draw(region, region.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	compact := compactTexturePixels(region)
	if compact.Bounds().Size() != image.Pt(1, 1) || compact.NRGBAAt(2, 3) != fill {
		t.Fatal("strided region did not preserve its uniform pixel")
	}
	region.SetNRGBA(7, 8, color.NRGBA{R: 20, G: 80, B: 161, A: 127})
	if compactTexturePixels(region) != region {
		t.Fatal("last-row color detail was discarded")
	}
	region.SetNRGBA(7, 8, fill)
	region.SetNRGBA(7, 3, color.NRGBA{R: 20, G: 80, B: 160, A: 128})
	if compactTexturePixels(region) != region {
		t.Fatal("first-row alpha detail was discarded")
	}
}

func TestStampAndSpriteShareCostumeTexture(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		pixels.SetNRGBA(512, 512, color.NRGBA{R: 160, G: 80, B: 40, A: 128})
		costume := &media.Costume{Image: pixels, Width: 16, Height: 12, CenterX: 8, CenterY: 6}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
		target.State.Effects.Color = 50
		pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
		dst := ebiten.NewImage(480, 360)
		defer dst.Deallocate()
		if err := gpu.Draw(dst, r, []vm.DisplayTarget{target}); err != nil {
			t.Fatal(err)
		}
		if len(gpu.textures) != 1 || gpu.bytes != 4<<20 {
			t.Fatalf("one costume used %d textures and %d bytes", len(gpu.textures), gpu.bytes)
		}
	})
}

func TestStageGPUCachePhysicalMemory(t *testing.T) {
	runGPUFrame(t, func() {
		var before ebiten.DebugInfo
		ebiten.ReadDebugInfo(&before)
		sizes := []image.Point{{1024, 1024}, {1025, 512}, {31, 31}}
		images := make([]*image.NRGBA, len(sizes))
		for i, size := range sizes {
			images[i] = image.NewNRGBA(image.Rectangle{Max: size})
			images[i].SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		}
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		for i := 0; i < 80; i++ {
			pixels := images[0]
			if i >= 40 {
				pixels = images[i%len(images)]
			}
			costume := &media.Costume{
				CacheKey: fmt.Sprintf("memory-check:%d", i),
				Image:    pixels, Width: float64(pixels.Bounds().Dx()), Height: float64(pixels.Bounds().Dy()),
			}
			texture, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if texture == nil || temporary {
				t.Fatal("ordinary costume was not cached")
			}
			// Flush uploads and evictions before measuring actual GPU storage.
			var sample [4]byte
			texture.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(sample[:])
			if sample != [4]byte{255, 0, 0, 255} {
				t.Fatalf("texture data changed: %v", sample)
			}
			if (i+1)%20 == 0 {
				var after ebiten.DebugInfo
				ebiten.ReadDebugInfo(&after)
				physical := after.TotalGPUImageMemoryUsageInBytes - before.TotalGPUImageMemoryUsageInBytes
				t.Logf("%d costumes: cached %.1f MiB, GPU %.1f MiB", i+1, float64(gpu.bytes)/(1<<20), float64(physical)/(1<<20))
				if physical > stageTextureBudget+(16<<20) {
					t.Errorf("bounded costume cache retained %d GPU bytes", physical)
				}
			}
		}
	})
}

func TestLargeBackgroundKeepsOtherTexturesDuringCameraMotion(t *testing.T) {
	runGPUFrame(t, func() {
		background := image.NewNRGBA(image.Rect(0, 0, 2960, 2720))
		draw.Draw(background, background.Bounds(), image.NewUniform(color.NRGBA{R: 60, G: 90, B: 120, A: 255}), image.Point{}, draw.Src)
		sprite := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		draw.Draw(sprite, sprite.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		lib := &media.Library{Costumes: [][]*media.Costume{{
			{Bitmap: true, Image: background, Width: 1480, Height: 1360, CenterX: 740, CenterY: 680},
			{Bitmap: true, Image: sprite, Width: 32, Height: 32, CenterX: 16, CenterY: 16},
		}}}
		r := render.New(lib)
		scene := []vm.DisplayTarget{
			{State: vm.TargetState{Visible: true, Size: 125, StretchX: 100, StretchY: 100, Direction: 90}},
			{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, CurrentCostume: 1}},
		}
		dst := ebiten.NewImage(480, 360)
		defer dst.Deallocate()
		var gpu stageGPU
		defer gpu.Close()
		retained := map[stageTextureKey]*ebiten.Image{}
		for frame := 0; frame < 8; frame++ {
			r.SetView(vm.View{Width: 480, Height: 360, Zoom: 100, Direction: 90, X: float64(frame)})
			if err := gpu.Draw(dst, r, scene); err != nil {
				t.Fatal(err)
			}
			if len(gpu.textures) < 2 {
				t.Fatal("large background evicted the other texture within the same frame")
			}
			for key, entry := range gpu.textures {
				if previous := retained[key]; previous != nil && previous != entry.image {
					t.Fatal("camera movement reuploaded an unchanged texture")
				}
				retained[key] = entry.image
			}
			if gpu.bytes > stageTextureBudget {
				t.Fatal("camera cache exceeded the existing memory budget")
			}
		}
		t.Logf("camera textures: %.2f MiB", float64(gpu.bytes)/(1<<20))
	})
}

func TestLargeBackgroundReusesTextureDuringRotation(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2960, 2720))
		costume := &media.Costume{Bitmap: true, Image: pixels, Width: 1480, Height: 1360, CenterX: 740, CenterY: 680}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		var gpu stageGPU
		defer gpu.Close()
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 125, StretchX: 100, StretchY: 100, Direction: 90}}
		uploaded := map[*ebiten.Image]bool{}
		bytes := 0
		for angle := 0; angle <= 360; angle += 3 {
			r.SetView(vm.View{Width: 480, Height: 360, Zoom: 100, Direction: float64(90 + angle)})
			sprite, ok := r.DisplaySprite(target, 480, 360)
			if !ok {
				t.Fatal("background is not visible")
			}
			texture, temporary := gpu.spriteTexture(r, &sprite, image.Pt(480, 360), vm.GraphicEffects{})
			if temporary || texture == nil {
				t.Fatal("background texture was not cached")
			}
			if !uploaded[texture] {
				uploaded[texture] = true
				bytes += texture.Bounds().Dx() * texture.Bounds().Dy() * 4
			}
		}
		t.Logf("one rotation: %d uploads, %.2f MiB transferred, %.2f MiB cached", len(uploaded), float64(bytes)/(1<<20), float64(gpu.bytes)/(1<<20))
		if len(uploaded) > 2 {
			t.Errorf("centered background rotation uploaded %d overlapping textures", len(uploaded))
		}
	})
}

func TestLargeBitmapZoomReusesUploadedFullTexture(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2304, 512))
		for y := 0; y < 512; y++ {
			for x := 0; x < 2304; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
			}
		}
		costume := &media.Costume{Bitmap: true, Image: pixels, Width: 1152, Height: 256, CenterX: 576, CenterY: 128}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		var gpu stageGPU
		defer gpu.Close()
		gpu.measureUploads = true
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		var full *ebiten.Image
		var retainedBytes int
		for i, zoom := range []float64{25, 100, 200, 25} {
			r.SetView(vm.View{Width: 480, Height: 360, Zoom: zoom, Direction: 90})
			sprite, ok := r.DisplaySprite(target, 480, 360)
			if !ok {
				t.Fatal("bitmap is not visible")
			}
			corners := sprite.Corners
			texture, temporary := gpu.spriteTexture(r, &sprite, image.Pt(480, 360), vm.GraphicEffects{})
			if texture == nil || temporary {
				t.Fatal("bitmap texture was not cached")
			}
			if i == 0 {
				full, retainedBytes = texture, gpu.bytes
			}
			if texture != full || sprite.Corners != corners {
				t.Errorf("zoom=%g replaced the cached full image with a crop", zoom)
			}
			if gpu.uploadCount != 1 || gpu.bytes != retainedBytes || gpu.evictions != 0 {
				t.Errorf("zoom=%g: uploads=%d bytes=%d evictions=%d; want one retained upload (%d bytes)", zoom, gpu.uploadCount, gpu.bytes, gpu.evictions, retainedBytes)
			}
		}
	})
}

func TestLargeBitmapViewportMatchesFullTexture(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2200, 2100))
		for y := 0; y < pixels.Bounds().Dy(); y++ {
			for x := 0; x < pixels.Bounds().Dx(); x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 7), G: uint8(y / 11), B: 120, A: uint8(100 + (x+y)%155)})
			}
		}
		costume := &media.Costume{Bitmap: true, Image: pixels, Width: 1100, Height: 1050, CenterX: 550, CenterY: 525}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{costume}}})
		view := vm.View{Width: 128, Height: 96, Zoom: 137, Direction: 73, X: 4.25, Y: -3.75}
		r.SetView(view)
		var gpu, referenceGPU stageGPU
		defer gpu.Close()
		defer referenceGPU.Close()
		if err := referenceGPU.initShader(); err != nil {
			t.Fatal(err)
		}
		full, temporary := referenceGPU.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if temporary {
			defer full.Deallocate()
		}
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		actual := ebiten.NewImage(128, 96)
		defer actual.Deallocate()
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 125, StretchX: 100, StretchY: 130, Direction: 90}}
		for _, effects := range []vm.GraphicEffects{
			{}, {Color: 40, Brightness: 20, Ghost: 30},
			{Whirl: 17}, {Fisheye: 50}, {Mosaic: 20}, {Pixelate: 10},
		} {
			target.State.Effects = effects
			for _, direction := range []float64{90, 37, -45} {
				target.State.Direction = direction
				for _, stretch := range []float64{100, -100} {
					target.State.StretchX = stretch
					for _, stamp := range []bool{false, true} {
						pen.Pen(vm.PenCommand{Kind: "clear"})
						var quad render.DisplaySprite
						if stamp {
							quad, _ = r.PenSprite(target)
							pen.Pen(vm.PenCommand{Kind: "stamp", Target: target})
							pen.flushStamps()
							pen.flushLines()
						} else {
							quad, _ = r.DisplaySprite(target, 128, 96)
							if err := gpu.Draw(actual, r, []vm.DisplayTarget{target}); err != nil {
								t.Fatal(err)
							}
						}
						got, want := make([]byte, 128*96*4), make([]byte, 128*96*4)
						source := actual
						if stamp {
							source = pen.image
						}
						source.ReadPixels(got)
						source.Clear()
						if !stamp {
							source.Fill(vm.ParseColor(view.Color))
						}
						if stamp && effects.Whirl == 0 && effects.Fisheye == 0 && effects.Mosaic == 0 && effects.Pixelate == 0 {
							pen.stamp(full, quad.Corners, effects, true)
							pen.flushStamps()
							pen.image.ReadPixels(want)
						} else {
							referenceGPU.quad(source, full, quad.Corners, costume.Width, costume.Height, effects, true, false)
							source.ReadPixels(want)
						}
						for i := range got {
							if delta := int(got[i]) - int(want[i]); delta < -1 || delta > 1 {
								t.Fatalf("stamp=%v direction=%g stretch=%g effects=%+v channel=%d: %d, want %d", stamp, direction, stretch, effects, i, got[i], want[i])
							}
						}
					}
				}
			}
		}
	})
}

func TestLargeBitmapZoomReleasesFullTextureUnderPressure(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 2200, 2100))
		for y := 0; y < 2100; y++ {
			for x := 0; x < 2200; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
			}
		}
		small := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		small.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		costume := &media.Costume{Bitmap: true, Image: pixels, Width: 1100, Height: 1050, CenterX: 550, CenterY: 525}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{{
			costume, {Bitmap: true, Image: small, Width: 32, Height: 32, CenterX: 16, CenterY: 16},
		}}})
		r.SetView(vm.View{Width: 128, Height: 96, Zoom: 100, Direction: 90})
		target := vm.DisplayTarget{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
		other := target
		other.State.CurrentCostume = 1
		dst := ebiten.NewImage(128, 96)
		defer dst.Deallocate()
		for _, backgroundFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("backgroundFirst=%v", backgroundFirst), func(t *testing.T) {
				var gpu stageGPU
				defer gpu.Close()
				full, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
				if full == nil || temporary || gpu.bytes != stageTextureBudget {
					t.Fatal("full bitmap did not fill the cache")
				}
				scene := []vm.DisplayTarget{other, target}
				if backgroundFirst {
					scene[0], scene[1] = scene[1], scene[0]
				}
				gpu.measureUploads = true
				for frame := 0; frame < 3; frame++ {
					gpu.uploadCount = 0
					if err := gpu.Draw(dst, r, scene); err != nil {
						t.Fatal(err)
					}
					if frame == 2 && gpu.uploadCount != 0 {
						t.Errorf("warm zoomed scene still uploads %d textures", gpu.uploadCount)
					}
				}
				if len(gpu.textures) != 2 || gpu.bytes >= stageTextureBudget {
					t.Errorf("full image prevented retaining crop and sprite: textures=%d bytes=%d", len(gpu.textures), gpu.bytes)
				}
			})
		}
	})
}

type textureUploadMemoryGame struct {
	t        *testing.T
	before   runtime.MemStats
	uploaded bool
	drawn    bool
}

func (g *textureUploadMemoryGame) Update() error {
	if !g.uploaded {
		runtime.GC()
		runtime.ReadMemStats(&g.before)
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		images := make([]*ebiten.Image, 20)
		for i := range images {
			images[i] = ebiten.NewImage(1024, 1024)
			stageUpload(images[i], pixels)
		}
		var sample [4]byte
		for _, im := range images {
			im.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(sample[:])
			im.Deallocate()
		}
		g.uploaded = true
	}
	if !g.drawn {
		return nil
	}
	// Upload buffers are released at frame end, not at ReadPixels.
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(g.before.HeapAlloc)
	g.t.Logf("retained heap after texture uploads: %.1f MiB", float64(retained)/(1<<20))
	if retained > 32<<20 {
		g.t.Errorf("texture upload buffers retained %d bytes; want less than 32 MiB", retained)
	}
	return ebiten.Termination
}

func (g *textureUploadMemoryGame) Draw(*ebiten.Image) {
	g.drawn = true
}

func (g *textureUploadMemoryGame) Layout(int, int) (int, int) {
	return 64, 48
}

func TestStageGPUUploadRetainedMemory(t *testing.T) {
	runGPUCheck(t, &textureUploadMemoryGame{t: t})
}

func TestStageGPUUploadPixels(t *testing.T) {
	runGPUFrame(t, func() {
		for _, strided := range []bool{false, true} {
			pixels := image.NewNRGBA(image.Rect(0, 0, 257, 263))
			if strided {
				parent := image.NewNRGBA(image.Rect(0, 0, 265, 271))
				pixels = parent.SubImage(image.Rect(3, 5, 260, 268)).(*image.NRGBA)
			}
			bounds := pixels.Bounds()
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					pixels.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x + y), uint8(x*7 + y*13)})
				}
			}
			for _, premultiplied := range []bool{false, true} {
				parent := ebiten.NewImage(265, 271)
				dst := parent.SubImage(image.Rect(2, 3, 259, 266)).(*ebiten.Image)
				stageUploadPixels(dst, pixels, premultiplied)
				want := make([]byte, 257*263*4)
				if premultiplied {
					rgba := &image.RGBA{Pix: want, Stride: 257 * 4, Rect: image.Rect(0, 0, 257, 263)}
					draw.Draw(rgba, rgba.Bounds(), pixels, bounds.Min, draw.Src)
				} else {
					for y := 0; y < 263; y++ {
						copy(want[y*257*4:(y+1)*257*4], pixels.Pix[y*pixels.Stride:y*pixels.Stride+257*4])
					}
				}
				got := make([]byte, len(want))
				dst.ReadPixels(got)
				for i, value := range want {
					if got[i] != value {
						t.Errorf("strided=%v premultiplied=%v byte %d: got %d, want %d", strided, premultiplied, i, got[i], value)
						break
					}
				}
				parent.Deallocate()
			}
		}
	})
}

func TestAnimationUploadsKeepSmallCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		uniform := image.NewNRGBA(image.Rect(0, 0, 1280, 960))
		draw.Draw(uniform, uniform.Bounds(), image.NewUniform(color.NRGBA{A: 255}), image.Point{}, draw.Src)
		bone := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		bone.SetNRGBA(16, 16, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		costumes := []*media.Costume{{CacheKey: "background", Image: uniform}, {CacheKey: "bone", Image: bone}}
		for _, costume := range costumes {
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		/*
			An animation larger than the GPU cache must not force tiny, reusable
			effects through decode/upload again when their next attack begins.
		*/
		frame := image.NewNRGBA(image.Rect(0, 0, 800, 450))
		frame.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		for i := 0; i < 40; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("animation:%d", i), Image: frame}
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		gpu.measureUploads = true
		for _, costume := range costumes {
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		if gpu.uploadCount != 0 {
			t.Fatalf("animation evicted reusable small costumes: %d repeated uploads", gpu.uploadCount)
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("costume preservation exceeded the cache budget: %d", gpu.bytes)
		}
	})
}

func TestRepeatedCostumesSurviveWorkingSetAboveBaseBudget(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		gpu := stageGPU{measureUploads: true}
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 512))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		costumes := make([]*media.Costume, 36)
		for i := range costumes {
			costumes[i] = &media.Costume{CacheKey: fmt.Sprintf("loop:%d", i), Image: pixels}
		}
		for round := 0; round < 4; round++ {
			gpu.uploadCount = 0
			for _, costume := range costumes {
				texture, owned := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
				if texture == nil {
					t.Fatal("cache pressure hid a costume")
				}
				if owned {
					texture.Deallocate()
				}
			}
			t.Logf("round %d: uploads=%d cache=%.1f MiB", round, gpu.uploadCount, float64(gpu.bytes)/(1<<20))
		}
		if gpu.uploadCount != 0 {
			t.Fatalf("repeated 72 MiB working set still uploads %d costumes per round", gpu.uploadCount)
		}
		if gpu.bytes > 128<<20 {
			t.Fatalf("repeated textures exceeded bounded headroom: %d", gpu.bytes)
		}
	})
}

func TestOneUseAnimationDoesNotDisplaceRepeatedCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		gpu := stageGPU{measureUploads: true}
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		costumes := make([]*media.Costume, 8)
		for i := range costumes {
			costumes[i] = &media.Costume{CacheKey: fmt.Sprintf("repeated:%d", i), Image: pixels}
		}
		for round := 0; round < 2; round++ {
			for _, costume := range costumes {
				gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			}
		}
		for i := 0; i < 24; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("one-use:%d", i), Image: pixels}
			texture, owned := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if owned {
				texture.Deallocate()
			}
		}
		gpu.uploadCount = 0
		for _, costume := range costumes {
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		if gpu.uploadCount != 0 {
			t.Errorf("one-use animation displaced %d repeated costumes", gpu.uploadCount)
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("one-use animation expanded the cache: %d", gpu.bytes)
		}
	})
}

func TestRepeatedTextureCacheReclaimsInactiveCostumes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 512))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		for round := 0; round < 3; round++ {
			for i := 0; i < 36; i++ {
				costume := &media.Costume{CacheKey: fmt.Sprintf("old:%d", i), Image: pixels}
				texture, owned := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
				if owned {
					texture.Deallocate()
				}
			}
		}
		if gpu.bytes <= stageTextureBudget {
			t.Fatal("test requires an expanded working set")
		}
		idle := &media.Costume{CacheKey: "idle", Image: image.NewNRGBA(image.Rect(0, 0, 1, 1))}
		for i := 0; i < 5000; i++ {
			texture, owned := gpu.texture(r, render.DisplaySprite{Costume: idle, Density: 1})
			if owned {
				texture.Deallocate()
			}
		}
		for _, name := range []string{"new", "old:0"} {
			costume := &media.Costume{CacheKey: name, Image: pixels}
			texture, owned := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if owned {
				texture.Deallocate()
			}
			if gpu.bytes > stageTextureBudget {
				t.Fatalf("inactive costumes or old eviction history kept extra cache space: %d", gpu.bytes)
			}
		}
	})
}

func TestRepeatedTextureCacheIsBounded(t *testing.T) {
	runGPUFrame(t, func() {
		var before ebiten.DebugInfo
		ebiten.ReadDebugInfo(&before)
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 512))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		for round := 0; round < 3; round++ {
			for i := 0; i < 80; i++ {
				costume := &media.Costume{CacheKey: fmt.Sprintf("large-set:%d", i), Image: pixels}
				texture, owned := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
				if gpu.bytes > 128<<20 {
					t.Fatalf("working set exceeded maximum cache: %d", gpu.bytes)
				}
				if i == 79 {
					var sample [4]byte
					texture.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(sample[:])
					if sample != [4]byte{255, 0, 0, 255} {
						t.Fatalf("oversized working set lost pixels: %v", sample)
					}
				}
				if owned {
					texture.Deallocate()
				}
			}
			var after ebiten.DebugInfo
			ebiten.ReadDebugInfo(&after)
			if physical := after.TotalGPUImageMemoryUsageInBytes - before.TotalGPUImageMemoryUsageInBytes; physical > 144<<20 {
				t.Fatalf("GPU storage exceeded bounded cache plus allocator overhead: %d", physical)
			}
		}
	})
}

func TestTextureReuseHistoryIsBounded(t *testing.T) {
	var gpu stageGPU
	for i := 0; i < 1000; i++ {
		gpu.clock++
		gpu.rememberTexture(stageTextureKey{image: fmt.Sprint(i)}, gpu.clock)
	}
	if len(gpu.evictedTextures) > 256 {
		t.Fatal("one-use animation retained unbounded eviction history")
	}
	if _, ok := gpu.evictedTextures[stageTextureKey{image: "0"}]; ok {
		t.Fatal("eviction history retained the oldest identity")
	}
	if _, ok := gpu.evictedTextures[stageTextureKey{image: "999"}]; !ok {
		t.Fatal("eviction history lost the most recent identity")
	}
}

func TestSmallCostumeProtectionStillReclaimsOldTextures(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		small := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		small.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		for i := 0; i < 80; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("small:%d", i), Image: small}
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		frame := image.NewNRGBA(image.Rect(0, 0, 800, 450))
		frame.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		for i := 0; i < 40; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("animation:%d", i), Image: frame}
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		if _, ok := gpu.textures[stageTextureKey{library: r.Assets, image: "small:0"}]; ok {
			t.Fatal("old small costumes were protected without a limit")
		}
		if _, ok := gpu.textures[stageTextureKey{library: r.Assets, image: "small:79"}]; !ok {
			t.Fatal("animation displaced even the recent small costumes")
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("cache exceeded the total budget: %d", gpu.bytes)
		}
	})
}

func TestAnimationReusesEvictedTextureStorage(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 800, 450))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		var first *ebiten.Image
		for i := 0; i < 32; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("frame:%d", i), Image: pixels}
			texture, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if i == 0 {
				first = texture
			}
		}
		// Leave a draw using the old pixels queued before the storage is reused.
		previous := ebiten.NewImage(1, 1)
		defer previous.Deallocate()
		previous.DrawImage(first, nil)
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pen.image = ebiten.NewImage(1, 1)
		pen.stamp(first, [4][2]float64{{0, 0}, {800, 0}, {800, 450}, {0, 450}}, vm.GraphicEffects{}, true)
		pen.queueStamps()
		pixels.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		costume := &media.Costume{CacheKey: "frame:32", Image: pixels}
		next, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if next != first || temporary {
			t.Fatal("same-size animation allocated another GPU image at eviction")
		}
		var penPixel [4]byte
		pen.image.ReadPixels(penPixel[:])
		if penPixel != [4]byte{255, 0, 0, 255} {
			t.Fatalf("eviction overwrote a deferred pen batch: %v", penPixel)
		}
		var oldPixel, newPixel [4]byte
		previous.ReadPixels(oldPixel[:])
		next.SubImage(image.Rect(0, 0, 1, 1)).(*ebiten.Image).ReadPixels(newPixel[:])
		if oldPixel != [4]byte{255, 0, 0, 255} || newPixel != [4]byte{0, 255, 0, 255} {
			t.Fatalf("texture reuse changed queued drawing: old=%v new=%v", oldPixel, newPixel)
		}
		if _, found := gpu.textures[stageTextureKey{library: r.Assets, image: "frame:0"}]; found {
			t.Fatal("evicted image retained an alias to reused storage")
		}
		if gpu.bytes != stageTextureBudget || len(gpu.textures) != 32 {
			t.Fatalf("texture accounting changed: bytes=%d textures=%d", gpu.bytes, len(gpu.textures))
		}
	})
}

func TestAnimationReusesStorageBeforeEvictingOtherSizes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		backgroundPixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		backgroundPixels.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
		background := &media.Costume{CacheKey: "background", Image: backgroundPixels}
		backgroundTexture, _ := gpu.texture(r, render.DisplaySprite{Costume: background, Density: 1})

		pixels := image.NewNRGBA(image.Rect(0, 0, 800, 450))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		var first *ebiten.Image
		for i := 0; i < 30; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("animation:%d", i), Image: pixels}
			texture, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if i == 0 {
				first = texture
			}
		}
		previous := ebiten.NewImage(1, 1)
		defer previous.Deallocate()
		previous.DrawImage(first, nil)
		pixels.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		gpu.measureUploads = true
		for i := 30; i < 60; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("animation:%d", i), Image: pixels}
			texture, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
			if i == 30 && (texture != first || temporary) {
				t.Fatal("animation discarded an unrelated texture instead of reusing its oldest frame")
			}
			gpu.texture(r, render.DisplaySprite{Costume: background, Density: 1})
		}
		cached, _ := gpu.texture(r, render.DisplaySprite{Costume: background, Density: 1})
		if cached != backgroundTexture || gpu.uploadCount != 30 {
			t.Fatal("streaming frames forced the background through another upload")
		}
		var oldPixel [4]byte
		previous.ReadPixels(oldPixel[:])
		if oldPixel != [4]byte{255, 0, 0, 255} {
			t.Fatalf("animation overwrote an earlier queued draw: %v", oldPixel)
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("reuse exceeded the cache budget: %d", gpu.bytes)
		}
	})
}

func TestTextureReuseReclaimsUnusedSizes(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		stale := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		stale.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
		for i := 0; i < 15; i++ {
			costume := &media.Costume{CacheKey: fmt.Sprintf("stale:%d", i), Image: stale}
			gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		}
		pixels := image.NewNRGBA(image.Rect(0, 0, 800, 800))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		frames := []*media.Costume{
			{CacheKey: "frame:a", Image: pixels},
			{CacheKey: "frame:b", Image: pixels},
		}
		for i := 0; i < 8; i++ {
			gpu.texture(r, render.DisplaySprite{Costume: frames[i%2], Density: 1})
		}
		gpu.measureUploads = true
		for i := 0; i < 8; i++ {
			gpu.texture(r, render.DisplaySprite{Costume: frames[i%2], Density: 1})
		}
		if gpu.uploadCount != 0 {
			t.Fatalf("unused sizes kept displacing active animation frames: %d uploads", gpu.uploadCount)
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("cache exceeded its memory budget: %d", gpu.bytes)
		}
	})
}

func TestRepeatedPixelTexturePreservesSampling(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 1280, 960))
		for y := 0; y < 960; y++ {
			for x := 0; x < 1280; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 2), G: uint8(y / 2), B: uint8((x/2 + y/2) * 7), A: uint8((x/2 + y/2) * 11)})
			}
		}
		costume := &media.Costume{Image: pixels, Bitmap: true, Width: 1280, Height: 960}
		r := render.New(&media.Library{})
		gpu := stageGPU{measureUploads: true}
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		compact, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if temporary || compact.Bounds().Size() != image.Pt(640, 480) || gpu.uploadBytes != 640*480*4 || gpu.bytes != 1024*512*4 {
			t.Fatalf("repeated pixels retained redundant storage: size=%v upload=%d cache=%d", compact.Bounds().Size(), gpu.uploadBytes, gpu.bytes)
		}
		full := ebiten.NewImageWithOptions(pixels.Bounds(), &ebiten.NewImageOptions{Unmanaged: true})
		defer full.Deallocate()
		stageUpload(full, pixels)
		for _, nearest := range []bool{false, true} {
			for _, corners := range [][4][2]float64{
				{{3.25, 4.5}, {83.25, 9.5}, {78.25, 73.5}, {-1.75, 68.5}},
				{{83.25, 9.5}, {3.25, 4.5}, {-1.75, 68.5}, {78.25, 73.5}},
			} {
				for _, effects := range []vm.GraphicEffects{{}, {Color: 78, Brightness: -20, Ghost: 37}, {Whirl: 70, Fisheye: -30, Mosaic: 30, Pixelate: 7}} {
					// Separate atlas offsets can change rasterization at pixel boundaries.
					got, want := ebiten.NewImageWithOptions(image.Rect(0, 0, 96, 80), &ebiten.NewImageOptions{Unmanaged: true}), ebiten.NewImageWithOptions(image.Rect(0, 0, 96, 80), &ebiten.NewImageOptions{Unmanaged: true})
					gpu.quad(got, compact, corners, 1280, 960, effects, nearest, false)
					gpu.quad(want, full, corners, 1280, 960, effects, nearest, false)
					a, b := make([]byte, 96*80*4), make([]byte, 96*80*4)
					got.ReadPixels(a)
					want.ReadPixels(b)
					if !bytes.Equal(a, b) {
						t.Fatalf("compacted sprite changed sampling: nearest=%v effects=%+v", nearest, effects)
					}
					if effects.Whirl == 0 && effects.Fisheye == 0 && effects.Mosaic == 0 && effects.Pixelate == 0 {
						got.Clear()
						want.Clear()
						original := pen.image
						pen.image = got
						pen.stamp(compact, corners, effects, nearest)
						pen.flushStamps()
						pen.image = want
						pen.stamp(full, corners, effects, nearest)
						pen.flushStamps()
						pen.image = original
						got.ReadPixels(a)
						want.ReadPixels(b)
						if !bytes.Equal(a, b) {
							t.Fatalf("compacted stamp changed sampling: nearest=%v effects=%+v", nearest, effects)
						}
					}
					got.Deallocate()
					want.Deallocate()
				}
			}
		}
	})
}

func TestRepeatedPixelCompactionPreservesEveryChannel(t *testing.T) {
	storage := image.NewNRGBA(image.Rect(0, 0, 520, 520))
	pixels := storage.SubImage(image.Rect(3, 5, 515, 517)).(*image.NRGBA)
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			pixels.SetNRGBA(x+3, y+5, color.NRGBA{R: uint8(x / 2), G: uint8(y / 2), B: 193, A: uint8((x/2 + y/2) * 11)})
		}
	}
	compact, repeated := media.CompactRepeatedPixels(pixels)
	if !repeated || compact.Bounds().Size() != image.Pt(256, 256) {
		t.Fatal("strided repeated bitmap was not compacted")
	}
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			if compact.NRGBAAt(x/2, y/2) != pixels.NRGBAAt(x+3, y+5) {
				t.Fatalf("lost pixel detail at %d,%d", x, y)
			}
		}
	}
	last := pixels.PixOffset(514, 516)
	for channel := 0; channel < 4; channel++ {
		pixels.Pix[last+channel] ^= 1
		if got, repeated := media.CompactRepeatedPixels(pixels); repeated || got != pixels {
			t.Fatalf("discarded last-pixel channel %d", channel)
		}
		pixels.Pix[last+channel] ^= 1
	}
}

func TestRepeatedTextureMetadataClearedAtReuse(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		// Nonuniform, but each 2x2 group is identical.
		draw.Draw(pixels, image.Rect(0, 0, 2, 2), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		costume := &media.Costume{CacheKey: "repeated", Image: pixels, Bitmap: true}
		first, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if !gpu.repeatedPixels[first] {
			t.Fatal("repeated texture has no sampling metadata")
		}
		/*
			Model a full cache; make the one occupied entry account for it so
			the next equally sized GPU allocation must evict and reuse it.
		*/
		for key, entry := range gpu.textures {
			entry.bytes = stageTextureBudget
			gpu.textures[key] = entry
		}
		gpu.bytes = stageTextureBudget
		ordinary := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		ordinary.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		costume = &media.Costume{CacheKey: "ordinary", Image: ordinary, Bitmap: true}
		next, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if next != first || gpu.repeatedPixels[next] || len(gpu.repeatedPixels) != 0 {
			t.Fatal("reused ordinary texture retained repeated-pixel metadata")
		}
	})
}

func BenchmarkCompactRepeatedPixels(b *testing.B) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 800, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 800; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 2), G: uint8(y / 2), A: 255})
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, repeated := media.CompactRepeatedPixels(pixels); !repeated {
			b.Fatal("repeated pixels were not recognized")
		}
	}
}

func TestRepeatedTextureDoesNotReuseAtlasStorage(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		var gpu stageGPU
		defer gpu.Close()
		ordinary := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		ordinary.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		costume := &media.Costume{CacheKey: "atlas", Image: ordinary, Bitmap: true}
		first, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		for key, entry := range gpu.textures {
			entry.bytes = stageTextureBudget
			gpu.textures[key] = entry
		}
		gpu.bytes = stageTextureBudget
		pixels := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		draw.Draw(pixels, image.Rect(0, 0, 2, 2), image.NewUniform(color.NRGBA{G: 255, A: 255}), image.Point{}, draw.Src)
		costume = &media.Costume{CacheKey: "repeated", Image: pixels, Bitmap: true}
		next, _ := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if next == first {
			t.Fatal("compressed standalone texture reused atlas storage")
		}
	})
}

func TestRuntimeCompactBitmapTexturePreservesDisplay(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		source := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				source.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 2), G: uint8(y / 2), B: 193, A: 127})
			}
		}
		uniform := image.NewNRGBA(source.Bounds())
		draw.Draw(uniform, uniform.Bounds(), image.NewUniform(color.NRGBA{R: 80, G: 120, A: 127}), image.Point{}, draw.Src)
		target := &project.Target{}
		for i, pixels := range []*image.NRGBA{source, uniform} {
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, pixels); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("frame%d.png", i)
			if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			target.Costumes = append(target.Costumes, project.Costume{MD5Ext: name, DataFormat: "png", BitmapResolution: 2})
		}
		lib, err := media.LoadRuntime(context.Background(), dir, &project.Project{Targets: []*project.Target{target}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		r := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		if err := gpu.initShader(); err != nil {
			t.Fatal(err)
		}
		costume := lib.Costumes[0][0]
		texture, temporary := gpu.texture(r, render.DisplaySprite{Costume: costume, Density: 1})
		if texture == nil || temporary || !gpu.repeatedPixels[texture] || texture.Bounds().Size() != image.Pt(256, 256) {
			t.Fatal("runtime compact bitmap was expanded before upload")
		}
		full := ebiten.NewImageWithOptions(source.Bounds(), &ebiten.NewImageOptions{Unmanaged: true})
		defer full.Deallocate()
		stageUpload(full, source)
		corners := [4][2]float64{{1.25, 2.5}, {62, 5}, {60, 46}, {-2, 40}}
		for _, nearest := range []bool{false, true} {
			for _, effects := range []vm.GraphicEffects{{}, {Color: 30, Brightness: 17, Ghost: 20}, {Whirl: 70, Fisheye: -30, Mosaic: 30, Pixelate: 7}} {
				/*
					Match destination atlas offsets so rasterization rounding does
					not differ independently of the source pixel representation.
				*/
				got := ebiten.NewImageWithOptions(image.Rect(0, 0, 64, 48), &ebiten.NewImageOptions{Unmanaged: true})
				want := ebiten.NewImageWithOptions(image.Rect(0, 0, 64, 48), &ebiten.NewImageOptions{Unmanaged: true})
				gpu.quad(got, texture, corners, costume.Width, costume.Height, effects, nearest, false)
				gpu.quad(want, full, corners, costume.Width, costume.Height, effects, nearest, false)
				a, b := make([]byte, 64*48*4), make([]byte, 64*48*4)
				got.ReadPixels(a)
				want.ReadPixels(b)
				got.Deallocate()
				want.Deallocate()
				if !bytes.Equal(a, b) {
					t.Fatalf("runtime compaction changed rendered pixels: nearest=%v effects=%+v", nearest, effects)
				}
			}
		}
		// Cropping must still use original coordinates, even at odd offsets.
		region := image.Rect(3, 5, 35, 37)
		cropped, _ := gpu.textureRegion(r, render.DisplaySprite{Costume: costume, Density: 1}, region)
		got := make([]byte, 32*32*4)
		cropped.ReadPixels(got)
		for y := 0; y < 32; y++ {
			start := source.PixOffset(3, y+5)
			if !bytes.Equal(got[y*32*4:(y+1)*32*4], source.Pix[start:start+32*4]) {
				t.Fatal("compact cache changed odd-origin crop coordinates")
			}
		}
		solid, _ := gpu.texture(r, render.DisplaySprite{Costume: lib.Costumes[0][1], Density: 1})
		if solid.Bounds().Size() != image.Pt(1, 1) || gpu.repeatedPixels[solid] {
			t.Fatal("uniform runtime bitmap retained repeated-pixel sampling metadata")
		}
	})
}

func TestSceneTexturesSurviveEarlierAnimationUpload(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 800, 450))
		draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		pixels.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		costumes := make([]*media.Costume, 34)
		for i := range costumes {
			costumes[i] = &media.Costume{CacheKey: fmt.Sprintf("scene:%d", i), Image: pixels, Width: 8, Height: 8, CenterX: 4, CenterY: 4}
		}
		r := render.New(&media.Library{Costumes: [][]*media.Costume{costumes}})
		r.SetView(vm.View{Width: 320, Height: 20, Zoom: 100, Direction: 90, Color: "#000000"})
		var gpu stageGPU
		defer gpu.Close()
		for i := 0; i < 32; i++ {
			gpu.texture(r, render.DisplaySprite{Costume: costumes[i], Density: 1})
		}
		/*
			A new animation frame draws before 31 existing visible costumes.
			The unused 32nd cached costume should make room, not those drawn later.
		*/
		scene := make([]vm.DisplayTarget, 32)
		for i := range scene {
			costume := i - 1
			if i == 0 {
				costume = 32
			}
			scene[i].State = vm.TargetState{CurrentCostume: costume, Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: float64(i*10 - 155)}
		}
		dst := ebiten.NewImage(320, 20)
		defer dst.Deallocate()
		gpu.measureUploads = true
		if err := gpu.Draw(dst, r, scene); err != nil {
			t.Fatal(err)
		}
		if gpu.uploadCount != 1 || gpu.evictions != 1 {
			t.Fatalf("visible costumes reuploaded: uploads=%d evictions=%d", gpu.uploadCount, gpu.evictions)
		}
		if gpu.bytes > stageTextureBudget {
			t.Fatalf("scene protection exceeded cache budget: %d", gpu.bytes)
		}
		for i := range scene {
			if got := color.RGBAModel.Convert(dst.At(i*10+5, 10)).(color.RGBA); got != (color.RGBA{R: 255, A: 255}) {
				t.Fatalf("sprite %d changed pixels: %v", i, got)
			}
		}
		/*
			Returning costumes may use bounded headroom rather than uploading
			the same overflow sprite on every frame.
		*/
		extra := scene[0]
		extra.State.CurrentCostume, extra.State.X, extra.State.Y = 31, 0, 5
		if err := gpu.Draw(dst, r, append(scene, extra)); err != nil {
			t.Fatal(err)
		}
		if gpu.bytes > 66<<20 || len(gpu.textures) > 33 {
			t.Fatal("visible scene retained more than its working set")
		}
		if gpu.protectTextures {
			t.Fatal("scene protection remained enabled after drawing")
		}
	})
}

func TestTextureProtectionDoesNotRasterizeText(t *testing.T) {
	r := render.New(&media.Library{Costumes: [][]*media.Costume{{}}})
	skin := &vm.TextSkin{Visible: true, Width: 200, Text: "changing text", Color: "#ffffff"}
	scene := []vm.DisplayTarget{{State: vm.TargetState{Visible: true, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, Text: skin}}}
	var gpu stageGPU
	allocations := testing.AllocsPerRun(10, func() {
		skin.Text += "."
		gpu.protectSceneTextures(r, scene, 480, 360)
	})
	/*
		Only the changed string above allocates. Cache eviction planning must
		not generate text pixels before the actual draw requests them.
	*/
	if allocations > 1 {
		t.Fatalf("texture protection prepared dynamic text: %.0f allocations", allocations)
	}
}

func TestDeferredStampTexturesSurviveCachePressure(t *testing.T) {
	runGPUFrame(t, func() {
		for _, drawScene := range []bool{false, true} {
			for _, count := range []int{32, 33, 34} {
				t.Run(fmt.Sprintf("scene=%v/count=%d", drawScene, count), func(t *testing.T) {
					pixels := image.NewNRGBA(image.Rect(0, 0, 1600, 640))
					draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
					draw.Draw(pixels, image.Rect(0, 0, 2, 2), image.NewUniform(color.NRGBA{G: 255, A: 255}), image.Point{}, draw.Src)
					costumes := make([]*media.Costume, 34)
					for i := range costumes {
						costumes[i] = &media.Costume{CacheKey: fmt.Sprintf("stamp:%d", i), Image: pixels, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4}
					}
					r := render.New(&media.Library{Costumes: [][]*media.Costume{costumes}})
					r.SetView(vm.View{Width: 340, Height: 20, Zoom: 100, Direction: 90, Color: "#000000", HighQualityPen: true})
					var gpu stageGPU
					defer gpu.Close()
					pen, err := gpu.enablePen(r)
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 32; i++ {
						gpu.texture(r, render.DisplaySprite{Costume: costumes[i], Density: 1})
					}
					dst := ebiten.NewImage(340, 20)
					defer dst.Deallocate()
					for frame := 0; frame < 4; frame++ {
						recorder := deferredPen{pen}
						recorder.Pen(vm.PenCommand{Kind: "clear"})
						for i := 0; i < count; i++ {
							costume := i - 1
							if i == 0 {
								costume = 32
							}
							if count == 34 {
								costume = (i + 32) % 34
							}
							/*
								Hidden sprites can still stamp. Their costumes are absent from the
								visible scene and must be protected using the recorded commands.
							*/
							target := vm.DisplayTarget{State: vm.TargetState{CurrentCostume: costume, Size: 100, StretchX: 100, StretchY: 100, Direction: 90, X: float64(i*10 - 165)}}
							recorder.Pen(vm.PenCommand{Kind: "stamp", Target: target})
							if i > 0 && i%8 == 0 {
								target.State.CurrentCostume = 32
								if count == 34 {
									target.State.CurrentCostume += (i / 8) % 2
								}
								target.State.X = -165
								recorder.Pen(vm.PenCommand{Kind: "stamp", Target: target})
							}
						}
						gpu.measureUploads = true
						gpu.uploadCount = 0
						gpu.evictions = 0
						if drawScene {
							if err := gpu.Draw(dst, r, nil); err != nil {
								t.Fatal(err)
							}
						} else {
							pen.replayCommands()
							pen.flushStamps()
							pen.flushLines()
							pen.flushClear([4]*ebiten.Image{})
							dst.Clear()
							gpu.drawPenImage(dst, pen.image, r, true)
						}
						if gpu.uploadCount > max(1, count-32) {
							t.Errorf("frame %d: repeated stamp uploads=%d evictions=%d", frame, gpu.uploadCount, gpu.evictions)
						}
						if len(gpu.transientTextures) != 0 {
							t.Error("frame retained transient texture")
						}
						if len(gpu.repeatedPixels) != len(gpu.textures) {
							t.Errorf("temporary texture retained: metadata=%d cached=%d", len(gpu.repeatedPixels), len(gpu.textures))
						}
						if gpu.bytes > max(64<<20, count*(2<<20)) {
							t.Fatalf("cache budget exceeded: %d", gpu.bytes)
						}
						if frame == 3 && gpu.uploadCount != 0 {
							t.Errorf("warmed stamp set still reuploads %d textures", gpu.uploadCount)
						}
						for i := 0; i < count; i++ {
							if got := color.RGBAModel.Convert(dst.At(i*10+5, 10)).(color.RGBA); got != (color.RGBA{R: 255, A: 255}) {
								t.Fatalf("stamp %d changed pixels: %v", i, got)
							}
						}
					}
				})
			}
		}
	})
}

func TestCroppedTextureSurvivesCachePressure(t *testing.T) {
	runGPUFrame(t, func() {
		for _, stamp := range []bool{false, true} {
			t.Run(fmt.Sprintf("stamp=%v", stamp), func(t *testing.T) {
				background := image.NewNRGBA(image.Rect(0, 0, 2200, 2100))
				for y := 0; y < 2100; y++ {
					for x := 0; x < 2200; x++ {
						background.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
					}
				}
				pixels := image.NewNRGBA(image.Rect(0, 0, 1600, 640))
				draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
				draw.Draw(pixels, image.Rect(0, 0, 2, 2), image.NewUniform(color.NRGBA{G: 255, A: 255}), image.Point{}, draw.Src)
				costumes := []*media.Costume{{CacheKey: "background", Image: background, Bitmap: true, Width: 1100, Height: 1050, CenterX: 550, CenterY: 525}}
				for i := 1; i <= 33; i++ {
					costumes = append(costumes, &media.Costume{CacheKey: fmt.Sprintf("filler:%d", i), Image: pixels, Bitmap: true, Width: 8, Height: 8, CenterX: 4, CenterY: 4})
				}
				costumes[33].Image = background.SubImage(image.Rect(0, 0, 768, 768)).(*image.NRGBA)
				r := render.New(&media.Library{Costumes: [][]*media.Costume{costumes}})
				r.SetView(vm.View{Width: 128, Height: 96, Zoom: 100, Direction: 90, Color: "#000000", HighQualityPen: true})
				var gpu stageGPU
				defer gpu.Close()
				pen, err := gpu.enablePen(r)
				if err != nil {
					t.Fatal(err)
				}
				target := vm.DisplayTarget{State: vm.TargetState{Visible: !stamp, Size: 100, StretchX: 100, StretchY: 100, Direction: 90}}
				d, ok := r.DisplaySprite(target, 128, 96)
				if !ok {
					t.Fatal("missing background")
				}
				original, _ := gpu.spriteTexture(r, &d, image.Pt(128, 96), vm.GraphicEffects{})
				var backgroundKey stageTextureKey
				for key := range gpu.textures {
					backgroundKey = key
				}
				if backgroundKey.region.Empty() {
					t.Fatal("background was not cropped")
				}
				for i := 1; gpu.bytes+(2<<20) <= stageTextureBudget; i++ {
					gpu.texture(r, render.DisplaySprite{Costume: costumes[i], Density: 1})
				}
				incoming := target
				incoming.State.CurrentCostume = 33
				dst := ebiten.NewImage(128, 96)
				defer dst.Deallocate()
				gpu.measureUploads = true
				if stamp {
					recorder := deferredPen{pen}
					recorder.Pen(vm.PenCommand{Kind: "clear"})
					recorder.Pen(vm.PenCommand{Kind: "stamp", Target: incoming})
					recorder.Pen(vm.PenCommand{Kind: "stamp", Target: target})
					if err := gpu.Draw(dst, r, nil); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := gpu.Draw(dst, r, []vm.DisplayTarget{incoming, target}); err != nil {
						t.Fatal(err)
					}
				}
				if got := gpu.textures[backgroundKey].image; got != original {
					t.Error("visible cropped background was evicted and uploaded again")
				}
				if gpu.uploadCount != 1 {
					t.Errorf("uploads=%d, want only the new costume", gpu.uploadCount)
				}
				if gpu.evictions == 0 {
					t.Error("test did not exercise cache pressure")
				}
				if gpu.bytes > stageTextureBudget {
					t.Errorf("cache exceeds budget: %d", gpu.bytes)
				}
			})
		}
	})
}

func TestTransientTextureReplacementPreservesQueuedStamps(t *testing.T) {
	runGPUFrame(t, func() {
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 32, Height: 16, Zoom: 100, Direction: 90, HighQualityPen: true})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 512))
		draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		pixels.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		for i := 0; i < 32; i++ {
			gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: fmt.Sprint(i), Image: pixels}, Density: 1})
		}
		gpu.cacheRenderer = r
		gpu.protectTextures = true
		for key, texture := range gpu.textures {
			texture.needed = true
			gpu.textures[key] = texture
		}
		first, owned := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "red", Image: pixels}, Density: 1})
		if owned || len(gpu.transientTextures) != 1 {
			t.Fatal("missing frame-owned transient")
		}
		pen.image = ebiten.NewImage(32, 16)
		pen.stamp(first, [4][2]float64{{0, 0}, {16, 0}, {16, 16}, {0, 16}}, vm.GraphicEffects{}, true)
		blue := image.NewNRGBA(pixels.Bounds())
		draw.Draw(blue, blue.Bounds(), image.NewUniform(color.NRGBA{B: 255, A: 255}), image.Point{}, draw.Src)
		blue.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
		second, owned := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "blue", Image: blue}, Density: 1})
		if owned || second == first || len(gpu.transientTextures) != 2 {
			t.Fatal("second transient was not retained")
		}
		pen.stamp(second, [4][2]float64{{16, 0}, {32, 0}, {32, 16}, {16, 16}}, vm.GraphicEffects{}, true)
		third, owned := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "third", Image: pixels}, Density: 1})
		if owned || third == first || gpu.transientBytes > stageTransientTextureLimit || len(gpu.transientTextures) != 2 {
			t.Fatal("transient replacement exceeded the shared budget")
		}
		gpu.releaseTransientTexture()
		for _, sample := range []struct {
			x    int
			want color.RGBA
		}{{8, color.RGBA{R: 255, A: 255}}, {24, color.RGBA{B: 255, A: 255}}} {
			if got := color.RGBAModel.Convert(pen.image.At(sample.x, 8)).(color.RGBA); got != sample.want {
				t.Fatalf("queued stamp at %d changed: %v", sample.x, got)
			}
		}
		/*
			Oversized caller-owned images must release the retained overflow
			first, rather than raising peak memory by the entire transient budget.
		*/
		gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "red", Image: pixels}, Density: 1})
		if len(gpu.transientTextures) == 0 {
			t.Fatal("test requires a retained transient before the oversized upload")
		}
		large := image.NewNRGBA(image.Rect(0, 0, 1025, 1025))
		large.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		texture, owned := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{CacheKey: "large", Image: large}, Density: 1})
		if !owned || len(gpu.transientTextures) != 0 {
			t.Fatal("oversized transient was retained")
		}
		texture.Deallocate()
		if gpu.bytes != stageTextureBudget {
			t.Fatalf("protected cache changed: %d", gpu.bytes)
		}
	})
}

func TestUnusedTextureEvictionKeepsPenBatchQueued(t *testing.T) {
	runGPUFrame(t, func() {
		for _, transient := range []bool{false, true} {
			for _, queued := range []bool{false, true} {
				func() {
					r := render.New(&media.Library{})
					var gpu stageGPU
					defer gpu.Close()
					pen, err := gpu.enablePen(r)
					if err != nil {
						t.Fatal(err)
					}
					pen.image = ebiten.NewImage(2, 2)
					pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
					draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
					cold := &media.Costume{CacheKey: "cold", Image: pixels}
					gpu.texture(r, render.DisplaySprite{Costume: cold, Density: 1})
					coldKey := stageTextureKey{library: r.Assets, image: "cold"}
					active := &media.Costume{CacheKey: "active", Image: pixels}
					texture, _ := gpu.texture(r, render.DisplaySprite{Costume: active, Density: 1})
					pen.stamp(texture, [4][2]float64{{0, 0}, {2, 0}, {2, 2}, {0, 2}}, vm.GraphicEffects{}, true)
					if queued {
						pen.queueStamps()
					}
					entry := gpu.textures[coldKey]
					if transient {
						delete(gpu.textures, coldKey)
						gpu.bytes -= entry.bytes
						gpu.transientTextures = map[stageTextureKey]stageTexture{coldKey: entry}
						gpu.transientBytes = entry.bytes
						gpu.evictTransientTexture(coldKey)
					} else {
						gpu.bytes += stageTextureBudget - entry.bytes
						entry.bytes = stageTextureBudget
						gpu.textures[coldKey] = entry
						next := &media.Costume{CacheKey: "next", Image: pixels}
						gpu.texture(r, render.DisplaySprite{Costume: next, Density: 1})
					}
					if _, exists := gpu.textures[coldKey]; exists {
						t.Fatal("cold texture was not evicted")
					}
					if len(pen.draws) == 0 && len(pen.stampVertices) == 0 {
						t.Fatalf("unrelated eviction submitted pending stamps: transient=%t queued=%t", transient, queued)
					}
					pen.flushStamps()
					if got := color.RGBAModel.Convert(pen.image.At(1, 1)); got != (color.RGBA{R: 255, A: 255}) {
						t.Fatalf("retained stamp changed pixels: %v", got)
					}
				}()
			}
		}
	})
}

func TestTransparentMarginGeometryPreservesPixels(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for y := 25; y < 39; y++ {
			for x := 28; x < 36; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 5), B: 150, A: uint8(30 + (x+y)%5*50)})
			}
		}
		lib := &media.Library{Costumes: [][]*media.Costume{{{Image: pixels, Width: 64, Height: 64, CenterX: 32, CenterY: 32}}}}
		r := render.New(lib)
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		texture, _ := gpu.texture(r, render.DisplaySprite{Costume: lib.Costumes[0][0], Density: 1})
		gotImage, wantImage := ebiten.NewImage(128, 128), ebiten.NewImage(128, 128)
		defer gotImage.Deallocate()
		defer wantImage.Deallocate()
		got, want := make([]byte, 128*128*4), make([]byte, 128*128*4)
		for _, angle := range []float64{0, 37, 90, -123} {
			for _, scale := range []float64{.5, 1, 1.75, 2, -1} {
				for _, shift := range []float64{0, .25} {
					var corners [4][2]float64
					si, co := math.Sincos(angle * math.Pi / 180)
					for i, point := range [4][2]float64{{-32, -32}, {32, -32}, {32, 32}, {-32, 32}} {
						x, y := point[0]*scale, point[1]*math.Abs(scale)
						corners[i] = [2]float64{64 + shift + co*x - si*y, 64 - shift + si*x + co*y}
					}
					for _, effects := range []vm.GraphicEffects{{}, {Ghost: 42}, {Color: 55, Brightness: -20}, {Brightness: 100}} {
						for mode := 0; mode < 8; mode++ {
							filter := float32(mode)
							nearest := mode%2 != 0
							original := stampQuadVertices(texture, corners, effects, filter)
							trimmed := original
							gpu.trimStampVertices(&trimmed, texture)
							options := &ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{texture}}
							gotImage.Fill(color.NRGBA{R: 20, G: 40, B: 60, A: 255})
							wantImage.Fill(color.NRGBA{R: 20, G: 40, B: 60, A: 255})
							gotImage.DrawTrianglesShader(trimmed[:], []uint16{0, 1, 2, 0, 2, 3}, pen.stampShader, options)
							wantImage.DrawTrianglesShader(original[:], []uint16{0, 1, 2, 0, 2, 3}, pen.stampShader, options)
							gotImage.ReadPixels(got)
							wantImage.ReadPixels(want)
							for i := range got {
								delta := int(got[i]) - int(want[i])
								if delta < -1 || delta > 1 || (nearest && delta != 0) {
									t.Fatalf("trim changed pixel %d: got=%d want=%d angle=%g scale=%g filter=%d shift=%g effects=%+v", i, got[i], want[i], angle, scale, mode, shift, effects)
								}
							}
						}
					}
				}
			}
		}
	})
}

func TestCulledQuadsPreserveViewportEdges(t *testing.T) {
	runGPUFrame(t, func() {
		pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		draw.Draw(pixels, image.Rect(28, 28, 36, 36), image.NewUniform(color.NRGBA{R: 180, G: 90, A: 130}), image.Point{}, draw.Src)
		r := render.New(&media.Library{})
		r.SetView(vm.View{Width: 64, Height: 64, Direction: 90, Zoom: 100})
		var gpu stageGPU
		defer gpu.Close()
		pen, err := gpu.enablePen(r)
		if err != nil {
			t.Fatal(err)
		}
		texture, _ := gpu.texture(r, render.DisplaySprite{Costume: &media.Costume{Image: pixels}, Density: 1})
		pen.image = ebiten.NewImage(64, 64)
		wantImage := ebiten.NewImage(64, 64)
		defer wantImage.Deallocate()
		got, want := make([]byte, 64*64*4), make([]byte, 64*64*4)
		for _, path := range []string{"pen", "sprite", "clipped sprite"} {
			clip := image.Rect(0, 0, 64, 64)
			if path == "clipped sprite" {
				clip = image.Rect(8, 4, 56, 60)
			}
			for _, angle := range []float64{0, 37, 90} {
				for _, scale := range []float64{1, -1.75} {
					for _, shift := range []float64{-40, -32, -29.25, 0, 29.25, 32, 40} {
						var corners [4][2]float64
						si, co := math.Sincos(angle * math.Pi / 180)
						for i, point := range [4][2]float64{{-32, -32}, {32, -32}, {32, 32}, {-32, 32}} {
							x, y := point[0]*scale, point[1]*math.Abs(scale)
							corners[i] = [2]float64{32 + shift + co*x - si*y, 32 + shift + si*x + co*y}
						}
						for _, nearest := range []bool{false, true} {
							pen.image.Fill(color.NRGBA{R: 30, G: 50, B: 90, A: 255})
							wantImage.Fill(color.NRGBA{R: 30, G: 50, B: 90, A: 255})
							effects := vm.GraphicEffects{Color: 37, Ghost: 20}
							filter := float32(boolInt(nearest))
							if path == "pen" {
								pen.stamp(texture, corners, effects, nearest)
								pen.flushStamps()
							} else {
								filter += 4
								gpu.quadVisual(clippedImage(pen.image, clip), texture, corners, 64, 64, effects, nearest, false, "", nil)
							}
							vertices := stampQuadVertices(texture, corners, effects, filter)
							clippedImage(wantImage, clip).DrawTrianglesShader(vertices[:], []uint16{0, 1, 2, 0, 2, 3}, pen.stampShader, &ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{texture}})
							pen.image.ReadPixels(got)
							wantImage.ReadPixels(want)
							for i := range got {
								delta := int(got[i]) - int(want[i])
								if delta < -1 || delta > 1 || (nearest && delta != 0) {
									t.Fatalf("%s changed pixel %d: got=%d want=%d angle=%g scale=%g shift=%g nearest=%t", path, i, got[i], want[i], angle, scale, shift, nearest)
								}
							}
						}
					}
				}
			}
		}
	})
}

func BenchmarkTextureContentBounds(b *testing.B) {
	for _, size := range []int{512, 2048} {
		for _, sparse := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d/sparse=%t", size, sparse), func(b *testing.B) {
				pixels := image.NewNRGBA(image.Rect(0, 0, size, size))
				content := pixels.Bounds()
				if sparse {
					content = image.Rect(size/2-8, size/2-8, size/2+8, size/2+8)
				}
				draw.Draw(pixels, content, image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if textureContentBounds(pixels).Empty() {
						b.Fatal("lost visible pixels")
					}
				}
			})
		}
	}
}

func TestTextureContentBoundsLimitsSparseScan(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	pixels.SetNRGBA(1024, 1024, color.NRGBA{R: 255, A: 255})
	if got := textureContentBounds(pixels); got != pixels.Bounds() {
		t.Fatalf("large sparse texture must retain full geometry instead of scanning every pixel: %v", got)
	}
}

func TestCostumeBlockPreparesBitmapBeforeDraw(t *testing.T) {
	runGPUFrame(t, func() {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "empty.svg"), []byte(`<svg width="1" height="1"></svg>`), 0600); err != nil {
			t.Fatal(err)
		}
		name := "selected.png"
		pixels := image.NewNRGBA(image.Rect(0, 0, 8, 4))
		pixels.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, pixels); err != nil {
			t.Fatal(err)
		}
		data := encoded.Bytes()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		p := &project.Project{Targets: []*project.Target{
			{IsStage: true, Name: "Stage"},
			{Name: "Stamper", Visible: false, Size: 100, Direction: 90, Costumes: []project.Costume{
				{MD5Ext: "empty.svg", DataFormat: "svg"}, {Name: "selected", MD5Ext: name, DataFormat: "png"},
			}},
		}}
		lib, err := media.LoadRuntime(context.Background(), dir, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer lib.Close()
		if err := lib.EnableDisplayPreparation(dir, p); err != nil {
			t.Fatal(err)
		}
		/*
			Add the script after loading so literal-name warmup cannot hide
			a missing runtime preparation callback.
		*/
		p.Targets[1].Blocks = map[string]*project.Block{
			"flag": {Opcode: "event_whenflagclicked", TopLevel: true, Next: "select"},
			"select": {Opcode: "looks_switchcostumeto", Inputs: map[string][]any{
				"COSTUME": {float64(1), []any{float64(10), "selected"}},
			}},
		}
		machine := vm.New(p)
		var gpu stageGPU
		defer gpu.Close()
		graphics, err := gpu.enablePen(render.New(lib))
		if err != nil {
			t.Fatal(err)
		}
		machine.SetGraphics(deferredPen{graphics})
		if err := machine.Run(context.Background(), 1, 60, false); err != nil {
			t.Fatal(err)
		}
		if machine.Snapshot()[1].CurrentCostume != 1 {
			t.Fatal("fixture did not select the costume")
		}
		if lib.PrepareDisplayBitmap(lib.Costumes[1][1]) {
			t.Fatal("bitmap decoding started only after the script finished")
		}
		if len(graphics.commands) != 0 || len(gpu.textures) != 0 {
			t.Fatal("costume selection executed pen or uploaded textures")
		}
	})
}
