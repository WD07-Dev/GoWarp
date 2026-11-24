// Package project reads Scratch 3 archives without extracting their assets.
package project

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type CustomFont struct {
	System bool   `json:"system"`
	Family string `json:"family"`
	MD5Ext string `json:"md5ext"`
}

type Project struct {
	CustomFonts      []CustomFont               `json:"customFonts"`
	ExtensionStorage map[string]json.RawMessage `json:"extensionStorage"`
	Targets          []*Target                  `json:"targets"`
	Monitors         []Monitor                  `json:"monitors"`
	Extensions       []string                   `json:"extensions"`
	ExtensionURLs    map[string]string          `json:"extensionURLs"`
}

type Monitor struct {
	Params     map[string]any `json:"params,omitempty"`
	SliderMin  *float64       `json:"sliderMin,omitempty"`
	SliderMax  *float64       `json:"sliderMax,omitempty"`
	IsDiscrete *bool          `json:"isDiscrete,omitempty"`
	ID         string         `json:"id"`
	Opcode     string         `json:"opcode"`
	Mode       string         `json:"mode"`
	SpriteName string         `json:"spriteName"`
	Visible    bool           `json:"visible"`
	X          float64        `json:"x"`
	Y          float64        `json:"y"`
	Width      float64        `json:"width"`
	Height     float64        `json:"height"`
}

type Costume struct {
	Name             string  `json:"name"`
	AssetID          string  `json:"assetId"`
	MD5Ext           string  `json:"md5ext"`
	DataFormat       string  `json:"dataFormat"`
	BitmapResolution float64 `json:"bitmapResolution"`
	RotationCenterX  float64 `json:"rotationCenterX"`
	RotationCenterY  float64 `json:"rotationCenterY"`
}

type Sound struct {
	Name        string  `json:"name"`
	AssetID     string  `json:"assetId"`
	MD5Ext      string  `json:"md5ext"`
	DataFormat  string  `json:"dataFormat"`
	Rate        float64 `json:"rate"`
	SampleCount float64 `json:"sampleCount"`
}

type Target struct {
	ExtensionStorage map[string]json.RawMessage `json:"extensionStorage,omitempty"`
	Comments         map[string]struct {
		Text    string `json:"text"`
		BlockID string `json:"blockId"`
	} `json:"comments"`
	Costumes       []Costume         `json:"costumes"`
	Sounds         []Sound           `json:"sounds"`
	CurrentCostume int               `json:"currentCostume"`
	LayerOrder     int               `json:"layerOrder"`
	Volume         float64           `json:"volume"`
	Tempo          float64           `json:"tempo"`
	Draggable      bool              `json:"draggable"`
	Size           float64           `json:"size"`
	Visible        bool              `json:"visible"`
	RotationStyle  string            `json:"rotationStyle"`
	Broadcasts     map[string]string `json:"broadcasts"`
	Name           string            `json:"name"`
	IsStage        bool              `json:"isStage"`
	Variables      map[string][]any  `json:"variables"`
	Lists          map[string][]any  `json:"lists"`
	Blocks         Blocks            `json:"blocks"`
	// ScriptOrder retains the saved top-level block order for event dispatch.
	ScriptOrder []string `json:"-"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	Direction   float64  `json:"direction"`
}

// Defaults match the reference loader while retaining explicit zero/false values.
func (t *Target) UnmarshalJSON(data []byte) error {
	type plain Target
	value := plain{
		Volume:        100,
		Tempo:         60,
		Direction:     90,
		Size:          100,
		RotationStyle: "all around",
	}
	decoded := struct {
		*plain
		Blocks json.RawMessage `json:"blocks"`
	}{plain: &value}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if len(decoded.Blocks) > 0 {
		var err error
		value.Blocks, value.ScriptOrder, err = decodeBlocks(decoded.Blocks)
		if err != nil {
			return err
		}
	}
	if value.IsStage {
		value.Visible = true
	}
	switch value.RotationStyle {
	case "left-right", "don't rotate":
	default:
		value.RotationStyle = "all around"
	}
	*t = Target(value)
	return nil
}

type Block struct {
	Mutation map[string]json.RawMessage `json:"mutation"`
	Opcode   string                     `json:"opcode"`
	Next     string                     `json:"next"`
	TopLevel bool                       `json:"topLevel"`
	Inputs   map[string][]any           `json:"inputs"`
	Fields   map[string][]any           `json:"fields"`
}

type Blocks map[string]*Block

func (b *Blocks) UnmarshalJSON(data []byte) error {
	blocks, _, err := decodeBlocks(data)
	if err == nil {
		*b = blocks
	}
	return err
}

func decodeBlocks(data []byte) (Blocks, []string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	blocks := make(Blocks)
	scripts := make([]string, 0)
	if token == nil {
		return blocks, scripts, nil
	}
	if token != json.Delim('{') {
		return nil, nil, fmt.Errorf("blocks must be an object")
	}
	var order []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		id := key.(string)
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, nil, err
		}
		// Scratch also stores unattached variable/list reporter arrays in blocks.
		if len(raw) > 0 && raw[0] == '[' {
			continue
		}
		var block *Block
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, nil, fmt.Errorf("block %q: %w", id, err)
		}
		if block == nil || block.Opcode == "" {
			return nil, nil, fmt.Errorf("block %q has no opcode", id)
		}
		if _, exists := blocks[id]; !exists {
			order = append(order, id)
		}
		blocks[id] = block
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil, err
	}
	for _, id := range order {
		if blocks[id].TopLevel {
			scripts = append(scripts, id)
		}
	}
	/*
		JavaScript enumerates array-index keys first, then other keys in saved
		order. Block IDs are usually nonnumeric, but either form is valid.
	*/
	index := func(id string) (uint64, bool) {
		n, err := strconv.ParseUint(id, 10, 32)
		return n, err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == id
	}
	sort.SliceStable(scripts, func(i, j int) bool {
		a, aIndex := index(scripts[i])
		b, bIndex := index(scripts[j])
		if aIndex != bIndex {
			return aIndex
		}
		return aIndex && a < b
	})
	return blocks, scripts, nil
}

// Undefined preserves a missing initial variable value separately from null.
type Undefined struct{}

func (Undefined) MarshalJSON() ([]byte, error) {
	return []byte(`"undefined"`), nil
}

func Load(path string) (*Project, error) {
	data, err := readManifest(path)
	if err != nil {
		return nil, err
	}

	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode project.json: %w", err)
	}
	stages := 0
	for i, target := range p.Targets {
		if target == nil {
			return nil, fmt.Errorf("target %d is null", i)
		}
		if target.IsStage {
			stages++
		}
		for id, list := range target.Lists {
			if len(list) < 1 {
				return nil, fmt.Errorf("target %q: invalid list %q", target.Name, id)
			}
			if _, ok := list[0].(string); !ok {
				return nil, fmt.Errorf("list %q has invalid name", id)
			}
			if len(list) == 1 {
				list = append(list, []any{})
				target.Lists[id] = list
			}
			items, ok := list[1].([]any)
			if !ok {
				items = []any{}
				list[1] = items
			}
			for _, item := range items {
				switch item.(type) {
				case nil, string, float64, bool:
				default:
					return nil, fmt.Errorf("list %q has invalid item", id)
				}
			}
		}
		for id, variable := range target.Variables {
			if len(variable) < 1 {
				return nil, fmt.Errorf("target %q: invalid variable %q", target.Name, id)
			}
			if _, ok := variable[0].(string); !ok {
				return nil, fmt.Errorf("variable %q has invalid name", id)
			}
			if len(variable) == 1 {
				variable = append(variable, Undefined{})
				target.Variables[id] = variable
			}
			switch variable[1].(type) {
			case nil, Undefined, string, float64, bool:
			default:
				return nil, fmt.Errorf("variable %q has invalid value", id)
			}
		}
	}
	if stages != 1 {
		return nil, fmt.Errorf("expected one stage, found %d", stages)
	}
	return &p, nil
}

func unpackedLocation(path string) (absolute, root, name string, err error) {
	absolute, err = filepath.Abs(path)
	if err != nil {
		return
	}
	info, statErr := os.Stat(absolute)
	if statErr != nil {
		err = statErr
		return
	}
	if info.IsDir() {
		root = absolute
		name = "project.json"
	} else if strings.EqualFold(filepath.Ext(absolute), ".json") {
		root = filepath.Dir(absolute)
		name = filepath.Base(absolute)
	}
	return
}

// OpenRoot confines symlinks as well as ordinary paths to the chosen project.
func readUnpackedFile(root, name string, limit int64) ([]byte, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, err := directory.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d MiB", name, limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d MiB", name, limit>>20)
	}
	return data, nil
}

func readManifest(path string) ([]byte, error) {
	absolute, root, name, err := unpackedLocation(path)
	if err != nil {
		return nil, err
	}
	if root != "" {
		return readUnpackedFile(root, name, 64<<20)
	}
	z, err := zip.OpenReader(absolute)
	if err != nil {
		return nil, fmt.Errorf("open SB3: %w", err)
	}
	defer z.Close()
	var manifest *zip.File
	for _, f := range z.File {
		if f.Name == "project.json" {
			if manifest != nil {
				return nil, fmt.Errorf("duplicate project.json")
			}
			manifest = f
		}
	}
	if manifest == nil {
		return nil, fmt.Errorf("SB3 has no project.json")
	}
	const maxManifest = 64 << 20
	if manifest.UncompressedSize64 > maxManifest {
		return nil, fmt.Errorf("project.json exceeds 64 MiB")
	}
	r, err := manifest.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxManifest+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifest {
		return nil, fmt.Errorf("project.json exceeds 64 MiB")
	}
	return data, nil
}
