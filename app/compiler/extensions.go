package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"gowarp/app/assets"
	"gowarp/app/host"
	"gowarp/app/i18n"
	"gowarp/core/extension"
	"gowarp/core/project"
)

const extensionSourceLimit = 16 << 20
const extensionSDKLimit = 256 << 20
const extensionCacheLimit = 512 << 20

type extensionSource struct {
	Ignored      bool
	ID           string
	Go           []byte
	Path         string
	Requirements []extension.ModuleRequirement
}

func resolveExtensionSources(ctx context.Context, input string, p *project.Project) ([]extensionSource, error) {
	var sources []extensionSource
	var failures []string
	seen := map[string]bool{}
	for _, id := range p.Extensions {
		if id == "pen" {
			continue
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate extension ID %q", id)
		}
		seen[id] = true
		location := p.ExtensionURLs[id]
		if location == "" {
			failures = append(failures, id+": no extensionURLs source")
			continue
		}
		data, path, err := readExtensionSource(ctx, input, location)
		var source extension.Source
		if err == nil {
			source, err = extension.ExtractSource(id+".js", data)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		sources = append(sources, extensionSource{ID: id, Go: source.Go, Path: path, Requirements: source.Requirements, Ignored: source.Ignored})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("Go extensions cannot be compiled:\n%s", strings.Join(failures, "\n"))
	}
	return sources, nil
}

func readSourceWithLimit(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("source exceeds %d-byte limit", limit)
	}
	return data, nil
}

func readExtensionSource(ctx context.Context, input, location string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if strings.HasPrefix(location, "data:") {
		head, body, ok := strings.Cut(location, ",")
		if !ok {
			return nil, "", fmt.Errorf("invalid data URL")
		}
		if len(body) > extensionSourceLimit*4 {
			return nil, "", fmt.Errorf("extension source exceeds limit")
		}
		decoded, err := url.PathUnescape(body)
		if err != nil {
			return nil, "", err
		}
		var r io.Reader = strings.NewReader(decoded)
		if strings.HasSuffix(strings.ToLower(head), ";base64") {
			r = base64.NewDecoder(base64.StdEncoding, r)
		}
		data, err := readSourceWithLimit(r, extensionSourceLimit)
		return data, "", err
	}
	parsed, err := url.Parse(location)
	if err != nil {
		return nil, "", err
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, "", err
		}
		client := &http.Client{Timeout: 30 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			return nil, "", err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("source HTTP status %s", response.Status)
		}
		data, err := readSourceWithLimit(response.Body, extensionSourceLimit)
		return data, "", err
	}
	if parsed.Scheme != "" {
		return nil, "", fmt.Errorf("unsupported extension source URL scheme %q", parsed.Scheme)
	}
	name, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return nil, "", err
	}
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return nil, "", fmt.Errorf("invalid project extension path %q", name)
	}
	if strings.EqualFold(filepath.Ext(input), ".sb3") {
		archive, err := zip.OpenReader(input)
		if err != nil {
			return nil, "", err
		}
		defer archive.Close()
		for _, entry := range archive.File {
			if entry.Name == name {
				r, err := entry.Open()
				if err != nil {
					return nil, "", err
				}
				defer r.Close()
				data, err := readSourceWithLimit(r, extensionSourceLimit)
				return data, "", err
			}
		}
		return nil, "", fmt.Errorf("extension source %q is absent from project archive", name)
	}
	root := filepath.Dir(input)
	if info, err := os.Stat(input); err == nil && info.IsDir() {
		root = input
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return nil, "", err
	}
	if !within(path, root) {
		return nil, "", fmt.Errorf("extension source escapes project directory")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := readSourceWithLimit(f, extensionSourceLimit)
	return data, path, err
}

func extensionRuntime(ctx context.Context, t Template, sources []extensionSource, output string, p *project.Project) (Template, error) {
	work, err := os.MkdirTemp("", "gowarp-sdk-*")
	if err != nil {
		return t, err
	}
	defer os.RemoveAll(work)
	sdk, err := findExtensionSDK(ctx, t, work, output)
	if err != nil {
		return t, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return t, err
	}
	cache = filepath.Join(cache, "gowarp", "extensions")
	if sdk == work {
		sdkHash, err := copyExtensionSDK(ctx, sdk, sdk)
		if err != nil {
			return t, err
		}
		return compileExtensionRuntime(ctx, t, sources, work, sdkHash, cache, p)
	}
	return buildExtensionRuntime(ctx, t, sources, sdk, cache, p)
}

func findExtensionSDK(ctx context.Context, t Template, work, output string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if data := assets.RuntimeSource; len(data) > 0 {
		hash := sha256.Sum256(data)
		if t.SDKHash != "" && hex.EncodeToString(hash[:]) != t.SDKHash {
			return "", fmt.Errorf("Go extension embedded SDK checksum/version mismatch: update GoWarp and its runtimes together")
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("embedded Go extension SDK: %w", err)
		}
		if err = unpackExtensionFiles(ctx, archive, work); err != nil {
			return "", err
		}
		return work, nil
	}
	roots := []string{filepath.Dir(filepath.Dir(t.Path)), filepath.Dir(filepath.Dir(filepath.Dir(t.Path)))}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	for _, root := range roots {
		archive := filepath.Join(root, "runtime-source.zip")
		if _, err := os.Stat(archive); err == nil {
			if output != "" {
				if err := protectOutput(output, []string{archive}); err != nil {
					return "", err
				}
			}
			if t.SDKHash != "" {
				file, err := os.Open(archive)
				if err != nil {
					return "", err
				}
				hash := sha256.New()
				_, err = io.Copy(hash, &contextReader{ctx: ctx, reader: file})
				file.Close()
				if err != nil {
					return "", err
				}
				if hex.EncodeToString(hash.Sum(nil)) != t.SDKHash {
					return "", fmt.Errorf("Go extension SDK checksum/version mismatch: %s", archive)
				}
			}
			if err = unpackExtensionSDK(ctx, archive, work); err != nil {
				return "", err
			}
			return work, nil
		}
	}
	if local, err := sourceRoot(); err == nil && t.SDKHash == "" {
		if _, err = os.Stat(filepath.Join(local, "core", "extension", "api.go")); err == nil {
			if output != "" && overlapsRuntimeSources(local, output) {
				return "", fmt.Errorf("output must differ from runtime SDK source files")
			}
			return local, nil
		}
	}
	return "", fmt.Errorf("%s", i18n.Tr("error.sdk_missing"))
}

func unpackExtensionSDK(ctx context.Context, path, destination string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("runtime-source.zip: %w", err)
	}
	defer archive.Close()
	return unpackExtensionFiles(ctx, &archive.Reader, destination)
}

func unpackExtensionFiles(ctx context.Context, archive *zip.Reader, destination string) error {
	var total int64
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(entry.Name)
		if entry.Mode()&os.ModeSymlink != 0 || filepath.IsAbs(name) || strings.Contains(entry.Name, "\\") || strings.Contains(entry.Name, ":") || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid SDK archive path %q", entry.Name)
		}
		target := filepath.Join(destination, name)
		if !within(target, destination) {
			return fmt.Errorf("SDK archive escapes destination")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if !isExtensionSDKFile(entry.Name) {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		data, err := readSourceWithLimit(reader, extensionSDKLimit-total)
		reader.Close()
		if err != nil {
			return err
		}
		total += int64(len(data))
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(target, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func isExtensionSDKFile(name string) bool {
	if name == "app/assets/embed/runtime-source.zip" {
		return false
	}
	if strings.Contains("/"+name+"/", "/testdata/") {
		return false
	}
	if name == "go.mod" || name == "go.sum" {
		return true
	}
	if !strings.HasPrefix(name, "app/") && !strings.HasPrefix(name, "core/") && !strings.HasPrefix(name, "platform/") {
		return false
	}
	return !strings.HasSuffix(name, "_test.go") && (strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".go.tmpl") || strings.HasSuffix(name, ".kage") || strings.HasPrefix(name, "app/assets/"))
}

func copyExtensionSDK(ctx context.Context, root, destination string) (string, error) {
	hash := sha256.New()
	var total int64
	for _, name := range []string{"go.mod", "go.sum", "app", "core", "platform"} {
		err := filepath.WalkDir(filepath.Join(root, name), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !isExtensionSDKFile(rel) {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("SDK contains symlink: %s", path)
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			data, err := readSourceWithLimit(file, extensionSDKLimit-total)
			file.Close()
			if err != nil {
				return err
			}
			total += int64(len(data))
			fmt.Fprintf(hash, "%d:%s:%d:", len(rel), rel, len(data))
			hash.Write(data)
			if root == destination {
				return nil
			}
			target := filepath.Join(destination, filepath.FromSlash(rel))
			if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			return os.WriteFile(target, data, 0644)
		})
		if err != nil {
			return "", err
		}
	}
	mod, err := os.ReadFile(filepath.Join(destination, "go.mod"))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(string(mod), "module gowarp\n") {
		return "", fmt.Errorf("runtime SDK module/version mismatch")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeExtensionMain(root string, sources []extensionSource) error {
	labels := strings.NewReplacer("\n", "_", "\r", "_", "/", "_", "\\", "_", ":", "_")
	for i, source := range sources {
		if source.Ignored {
			continue
		}
		name := fmt.Sprintf("extension%d", i)
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, source.ID+".js (Go comment)", source.Go, parser.AllErrors)
		if err != nil {
			return err
		}
		start := positions.Position(file.Name.Pos()).Offset
		end := positions.Position(file.Name.End()).Offset
		code := string(source.Go[:start]) + name + string(source.Go[end:])
		dir := filepath.Join(root, "generatedextensions", name)
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		label := labels.Replace(source.ID)
		data := []byte("//line " + label + ".js-go:1\n" + code)
		if err = os.WriteFile(filepath.Join(dir, "extension.go"), data, 0644); err != nil {
			return err
		}
	}
	return writeExtensionProgram(root, "generatedplayer", assets.ExtensionPlayerTemplate, sources)
}

func extensionBuildEnv(t Template) ([]string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(cache, "gowarp", "go")
	overrides := map[string]string{
		"GOOS": t.OS, "GOARCH": t.Arch, "CGO_ENABLED": "0",
		"GOWORK": "off", "GOFLAGS": "", "GOENV": "off", "GOEXPERIMENT": "",
		"GOAMD64": "v1", "GOARM64": "v8.0", "GO386": "sse2",
		"GOPATH": root, "GOMODCACHE": filepath.Join(root, "pkg", "mod"),
		"GOCACHE": filepath.Join(root, "cache"), "GOBIN": filepath.Join(root, "bin"),
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[strings.ToUpper(key)]; !ok {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env, nil
}

var extensionBuildMu sync.Mutex
var lastExtensionBuild struct {
	request string
	binary  string
}

func buildExtensionRuntime(ctx context.Context, t Template, sources []extensionSource, sdk, cache string, p *project.Project) (Template, error) {
	if err := ctx.Err(); err != nil {
		return t, err
	}
	work, err := os.MkdirTemp("", "gowarp-extensions-*")
	if err != nil {
		return t, err
	}
	defer os.RemoveAll(work)
	sdkHash, err := copyExtensionSDK(ctx, sdk, work)
	if err != nil {
		return t, fmt.Errorf("Go extension SDK: %w", err)
	}
	return compileExtensionRuntime(ctx, t, sources, work, sdkHash, cache, p)
}

func compileExtensionRuntime(ctx context.Context, t Template, sources []extensionSource, work, sdkHash, cache string, p *project.Project) (Template, error) {
	requirements, err := collectExtensionModules(sources)
	if err != nil {
		return t, err
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		return t, fmt.Errorf(i18n.Tr("error.go_missing"), err)
	}
	command := host.Command(ctx, goTool, "version")
	command.Dir = work
	buildEnv, err := extensionBuildEnv(t)
	if err != nil {
		return t, err
	}
	command.Env = buildEnv
	version, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return t, ctx.Err()
		}
		return t, fmt.Errorf("Go toolchain: %w\n%s", err, version)
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "gowarp-go-build-7\n%s\n%s\n%s/%s\nhost=%s/%s\n%d\n", sdkHash, version, t.OS, t.Arch, runtime.GOOS, runtime.GOARCH, t.Version)
	for _, source := range sources {
		fmt.Fprintf(hash, "%d:%s:%t:%d:", len(source.ID), source.ID, source.Ignored, len(source.Go))
		hash.Write(source.Go)
	}
	for _, requirement := range requirements {
		fmt.Fprintf(hash, "\nrequire %s %s\n", requirement.Path, requirement.Version)
	}
	validatorName := hex.EncodeToString(hash.Sum(nil)) + "-validator"
	if runtime.GOOS == "windows" {
		validatorName += ".exe"
	}
	validator := filepath.Join(cache, validatorName)
	if err = os.MkdirAll(cache, 0700); err != nil {
		return t, err
	}
	for !extensionBuildMu.TryLock() {
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer extensionBuildMu.Unlock()
	projectData, err := json.Marshal(preparationRequest(p))
	if err != nil {
		return t, err
	}
	requestHash := sha256.New()
	requestHash.Write(hash.Sum(nil))
	requestHash.Write(projectData)
	requestKey := hex.EncodeToString(requestHash.Sum(nil))
	if lastExtensionBuild.request == requestKey {
		cached := filepath.Join(cache, lastExtensionBuild.binary)
		info, playerErr := os.Stat(cached)
		check, validatorErr := os.Stat(validator)
		if playerErr == nil && info.Size() > 0 && validatorErr == nil && check.Size() > 0 {
			if p != nil {
				if err = prepareExtensionProject(ctx, validator, p, ""); err != nil {
					return t, err
				}
			}
			t.validatorPath = validator
			return extensionTemplate(t, cached)
		}
	}
	validatorBinary := validator
	validatorInfo, validatorErr := os.Stat(validator)
	buildValidator := validatorErr != nil || validatorInfo.Size() == 0
	if buildValidator {
		if err = writeExtensionMain(work, sources); err != nil {
			return t, err
		}
		if err = writeExtensionProgram(work, "generatedvalidator", assets.ExtensionValidatorTemplate, sources); err != nil {
			return t, err
		}
		if err = prepareExtensionModules(ctx, goTool, work, buildEnv, requirements); err != nil {
			return t, fmt.Errorf("extensions %s: %w", extensionIDs(sources), err)
		}
		validatorBinary = filepath.Join(work, validatorName)
		validatorTarget := t
		validatorTarget.OS, validatorTarget.Arch = runtime.GOOS, runtime.GOARCH
		hostEnv, err := extensionBuildEnv(validatorTarget)
		if err != nil {
			return t, err
		}
		command = host.Command(ctx, goTool, "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-o", validatorBinary, "./generatedvalidator")
		command.Dir, command.Env = work, hostEnv
		if output, buildErr := command.CombinedOutput(); buildErr != nil {
			if ctx.Err() != nil {
				return t, ctx.Err()
			}
			return t, fmt.Errorf("Go extension validator compilation failed (%s): %w\n%s", extensionIDs(sources), buildErr, output)
		}
	}
	var native []byte
	if p != nil {
		nativePath := filepath.Join(work, "core", "vm", "native_project_generated.go")
		if err = os.MkdirAll(filepath.Dir(nativePath), 0755); err != nil {
			return t, err
		}
		if err = prepareExtensionProject(ctx, validatorBinary, p, nativePath); err != nil {
			return t, err
		}
		native, err = os.ReadFile(nativePath)
		if err != nil && !os.IsNotExist(err) {
			return t, err
		}
	}
	fmt.Fprintf(hash, "\nnative:%d:", len(native))
	hash.Write(native)
	name := hex.EncodeToString(hash.Sum(nil))
	if t.OS == "windows" {
		name += ".exe"
	}
	cached := filepath.Join(cache, name)
	if info, e := os.Stat(cached); e == nil && info.Size() > 0 {
		if buildValidator {
			if err = publishExtensionBinary(ctx, validatorBinary, validator); err != nil {
				return t, err
			}
		}
		lastExtensionBuild.request, lastExtensionBuild.binary = requestKey, name
		t.validatorPath = validator
		return extensionTemplate(t, cached)
	}
	if !buildValidator {
		if err = writeExtensionMain(work, sources); err != nil {
			return t, err
		}
		if err = prepareExtensionModules(ctx, goTool, work, buildEnv, requirements); err != nil {
			return t, fmt.Errorf("extensions %s: %w", extensionIDs(sources), err)
		}
	}
	binary := filepath.Join(work, "player")
	linkerFlags := "-s -w"
	if t.OS == "windows" {
		linkerFlags += " -H=windowsgui"
	}
	args := []string{"build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-ldflags=" + linkerFlags}
	args = append(args, "-o", binary, "./generatedplayer")
	command = host.Command(ctx, goTool, args...)
	command.Dir = work
	command.Env = buildEnv
	output, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return t, ctx.Err()
		}
		return t, fmt.Errorf("Go extension compilation failed (%s): %w\n%s", extensionIDs(sources), err, output)
	}
	if err = ctx.Err(); err != nil {
		return t, err
	}
	playerInfo, err := os.Stat(binary)
	if err != nil {
		return t, err
	}
	validatorInfo, err = os.Stat(validatorBinary)
	if err != nil {
		return t, err
	}
	if playerInfo.Size()+validatorInfo.Size() > extensionCacheLimit {
		return t, fmt.Errorf("compiled extension player and validator exceed cache limit")
	}
	if err = publishExtensionBinary(ctx, binary, cached); err != nil {
		return t, err
	}
	if buildValidator {
		if err = publishExtensionBinary(ctx, validatorBinary, validator); err != nil {
			return t, err
		}
	}
	trimExtensionCache(cache, cached, validator)
	lastExtensionBuild.request, lastExtensionBuild.binary = requestKey, name
	t.validatorPath = validator
	return extensionTemplate(t, cached)
}

func publishExtensionBinary(ctx context.Context, binary, cached string) error {
	file, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 || info.Size() > extensionCacheLimit {
		return fmt.Errorf("compiled extension runtime must be nonempty and no larger than %d MiB", extensionCacheLimit>>20)
	}
	staged, err := os.CreateTemp(filepath.Dir(cached), ".building-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	if _, err = io.Copy(staged, &contextReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if err = staged.Chmod(0755); err != nil {
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), cached)
}

func extensionIDs(sources []extensionSource) string {
	ids := make([]string, len(sources))
	for i, s := range sources {
		ids[i] = s.ID
	}
	return strings.Join(ids, ", ")
}

func extensionTemplate(t Template, path string) (Template, error) {
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return t, err
	}
	t.Path = path
	t.SHA256 = hex.EncodeToString(h.Sum(nil))
	return t, nil
}

func trimExtensionCache(root string, keep ...string) {
	entries, _ := os.ReadDir(root)
	type item struct {
		path string
		info fs.FileInfo
	}
	var files []item
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, item{filepath.Join(root, entry.Name()), info})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	remaining := len(files)
	for _, file := range files {
		if total <= extensionCacheLimit && remaining <= 16 {
			break
		}
		if slices.Contains(keep, file.path) {
			continue
		}
		if os.Remove(file.path) == nil {
			total -= file.info.Size()
			remaining--
		}
	}
}
