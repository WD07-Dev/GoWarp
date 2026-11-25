package packager

import (
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"

	"gowarp/app/assets"
	"gowarp/app/config"
	"gowarp/app/i18n"
)

type field struct {
	label, key, value, kind string
	section                 int
	choices                 []string
}

type form struct {
	base                      config.Settings
	fields                    []field
	icon                      image.Image
	loadingLogo, loadingImage *assets.LoadingImage
}

func newForm(s config.Settings, variables map[string]string) *form {
	s.Window.Width, s.Window.Height = s.DisplaySize()
	if s.HighQualityPen == nil {
		s.HighQualityPen = new(bool)
	}
	if s.RemoveFencing == nil {
		s.RemoveFencing = new(bool)
	}
	if s.RemoveMiscLimits == nil {
		s.RemoveMiscLimits = new(bool)
	}
	f := &form{base: s}
	f.icon, _ = assets.DecodeIcon(s.Player.Icon)
	f.loadingLogo, _ = assets.DecodeLoadingImage(s.Loading.Logo)
	f.loadingImage, _ = assets.DecodeLoadingImage(s.Loading.Image)
	data, _ := json.Marshal(s)
	var model map[string]any
	json.Unmarshal(data, &model)
	add := func(section int, key, label string, choices ...string) {
		parts := strings.Split(key, ".")
		var v any = model
		for _, p := range parts {
			v = v.(map[string]any)[p]
		}
		if v == nil {
			v = ""
		}
		kind := "string"
		switch v.(type) {
		case float64:
			kind = "number"
		case bool:
			kind = "bool"
			choices = []string{"true", "false"}
		}
		f.fields = append(f.fields, field{
			label:   label,
			key:     key,
			value:   fmt.Sprint(v),
			kind:    kind,
			section: section,
			choices: choices,
		})
	}
	add(0, "project", "field.project")
	add(0, "appId", "field.app_id")
	add(1, "fps", "field.fps")
	add(1, "turbo", "field.turbo")
	add(1, "interpolation", "field.interpolation")
	add(1, "highQualityPen", "field.hq_pen")
	add(1, "removeFencing", "field.fencing")
	add(1, "removeMiscLimits", "field.misc_limits")
	add(2, "window.width", "field.width")
	add(2, "window.height", "field.height")
	add(2, "canvas.antialias", "field.antialias")
	add(2, "player.renderFPS", "field.render_fps", "0", "30", "60")
	add(2, "window.fit", "field.fit", "preserve", "integer", "stretch")
	add(2, "window.background", "field.background")
	add(3, "player.title", "field.title")
	f.fields = append(f.fields, field{
		label:   "field.icon",
		key:     "player.icon",
		kind:    "icon",
		section: 3,
	})
	add(3, "player.autoStart", "field.autostart")
	add(3, "player.closeOnStop", "field.close_on_stop")
	add(3, "player.controls", "field.controls")
	add(3, "player.mute", "field.mute")
	add(3, "player.username", "field.username")
	add(3, "player.accent", "field.accent")
	add(4, "cloud.mode", "field.cloud_mode", "off", "local", "websocket")
	add(4, "cloud.provider", "field.cloud_provider", "turbowarp", "custom")
	add(4, "cloud.server", "field.cloud_server")
	add(4, "cloud.projectId", "field.cloud_id")
	add(4, "cloud.contact", "field.cloud_contact")
	add(5, "loading.background", "field.loading_background")
	add(5, "loading.text", "field.loading_text")
	add(5, "loading.showProgress", "field.loading_show_progress")
	add(5, "loading.progress", "field.loading_progress")
	add(5, "loading.width", "field.loading_width")
	add(5, "loading.height", "field.loading_height")
	f.fields = append(f.fields,
		field{label: "field.loading_logo", key: "loading.logo", kind: "loadingImage", section: 5},
		field{label: "field.loading_image", key: "loading.image", kind: "loadingImage", section: 5},
		field{label: "field.loading_preview", kind: "loadingPreview", section: 5},
	)
	add(6, "platform.resizable", "field.resizable")
	add(6, "platform.disableClose", "field.no_close")
	add(6, "platform.disableMinimize", "field.no_minimize")
	add(6, "platform.disableMaximize", "field.no_maximize")
	add(6, "platform.fullscreen", "field.fullscreen")
	add(6, "platform.border", "field.border")
	add(6, "platform.topmost", "field.topmost")
	add(6, "platform.multiWindow", "field.multiwindow")
	ids := make([]string, 0, len(variables))
	for id := range variables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		mode := s.Cloud.Variables[id]
		if mode == "" {
			mode = "inherit"
		}
		f.fields = append(f.fields, field{
			label:   variables[id],
			key:     id,
			value:   mode,
			kind:    "variable",
			section: 4,
			choices: []string{"inherit", "off", "local", "websocket"},
		})
	}
	return f
}

func (f *form) visibleFields(section int, targetOS string) []int {
	usesNetwork := false
	showsControls := false
	showsProgress := false
	provider := ""
	for _, field := range f.fields {
		if field.kind == "variable" {
			usesNetwork = usesNetwork || field.value == "websocket"
			continue
		}
		switch field.key {
		case "cloud.mode":
			usesNetwork = usesNetwork || field.value == "websocket"
		case "cloud.provider":
			provider = field.value
		case "player.controls":
			showsControls = field.value == "true"
		case "loading.showProgress":
			showsProgress = field.value == "true"
		}
	}

	var visible []int
	for i, field := range f.fields {
		if field.section != section {
			continue
		}
		if section == 6 && (targetOS != "windows" && targetOS != "linux" || field.key == "platform.multiWindow" && targetOS != "windows") {
			continue
		}
		if field.kind != "variable" {
			switch field.key {
			case "cloud.provider", "cloud.projectId":
				if !usesNetwork {
					continue
				}
			case "cloud.server":
				if !usesNetwork || provider != "custom" {
					continue
				}
			case "cloud.contact":
				if !usesNetwork || provider != "turbowarp" {
					continue
				}
			case "player.accent":
				if !showsControls {
					continue
				}
			case "loading.progress":
				if !showsProgress {
					continue
				}
			}
		}
		visible = append(visible, i)
	}
	return visible
}

func (f *form) settings() (config.Settings, error) {
	data, _ := json.Marshal(f.base)
	var model map[string]any
	json.Unmarshal(data, &model)
	variables := map[string]string{}
	for _, field := range f.fields {
		switch field.kind {
		case "icon", "loadingImage", "loadingPreview":
			continue
		case "variable":
			if field.value != "inherit" {
				variables[field.key] = field.value
			}
			continue
		}
		var value any = field.value
		var err error
		switch field.kind {
		case "number":
			value, err = strconv.ParseFloat(field.value, 64)
		case "bool":
			value, err = strconv.ParseBool(field.value)
		}
		if err != nil {
			return config.Settings{}, fmt.Errorf("%s: %w", i18n.Tr(field.label), err)
		}
		parts := strings.Split(field.key, ".")
		dest := model
		for _, p := range parts[:len(parts)-1] {
			dest = dest[p].(map[string]any)
		}
		dest[parts[len(parts)-1]] = value
	}
	model["cloud"].(map[string]any)["variables"] = variables
	data, err := json.Marshal(model)
	if err != nil {
		return config.Settings{}, err
	}
	var s config.Settings
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	s.Window.Width, s.Window.Height = s.DisplaySize()
	return s, s.Validate()
}

// settingsForPlatform applies target restrictions without changing the saved project.
func settingsForPlatform(s config.Settings, targetOS string) config.Settings {
	if targetOS != "windows" {
		s.Platform.MultiWindow = false
	}
	return s
}
