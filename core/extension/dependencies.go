package extension

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type ModuleRequirement struct {
	Path    string
	Version string
}

var modulePath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
var moduleVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+incompatible)?$`)

func parseRequirements(name, config string) ([]ModuleRequirement, error) {
	var requirements []ModuleRequirement
	versions := make(map[string]string)
	for _, line := range strings.Split(config, "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || fields[0] != "require" {
			return nil, fmt.Errorf("%s: @gowarp-config expects require module/path v1.2.3", name)
		}
		module, version := fields[1], fields[2]
		first, _, _ := strings.Cut(module, "/")
		if !modulePath.MatchString(module) || !strings.Contains(first, ".") ||
			path.Clean(module) != module || strings.Contains(module, "..") ||
			strings.HasPrefix(module, ".") || strings.HasSuffix(module, "/") {
			return nil, fmt.Errorf("%s: invalid external module path %q", name, module)
		}
		if !moduleVersion.MatchString(version) {
			return nil, fmt.Errorf("%s: module %q needs a full version, such as v1.2.3 or a Go pseudo-version; got %q", name, module, version)
		}
		if previous, ok := versions[module]; ok {
			if previous != version {
				return nil, fmt.Errorf("%s: conflicting versions for %s: %s and %s", name, module, previous, version)
			}
			continue
		}
		versions[module] = version
		requirements = append(requirements, ModuleRequirement{Path: module, Version: version})
	}
	return requirements, nil
}
