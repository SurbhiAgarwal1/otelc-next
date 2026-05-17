package versions

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Dependency represents a resolved module and its version.
type Dependency struct {
	Path    string
	Version string
}

// ModuleInfo holds module information for the target project.
type ModuleInfo struct {
	Name         string
	Dependencies map[string]string
}

// FindAndParseGoMod walks up from the starting directory to find go.mod and parses it.
func FindAndParseGoMod(startDir string) (*ModuleInfo, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		dir = startDir
	}

	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return ParseGoMod(goModPath)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return &ModuleInfo{Dependencies: make(map[string]string)}, nil
}

// ParseGoMod parses a go.mod file to extract module name and dependency versions.
func ParseGoMod(path string) (*ModuleInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info := &ModuleInfo{
		Dependencies: make(map[string]string),
	}

	scanner := bufio.NewScanner(file)
	inRequire := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "module ") {
			info.Name = strings.TrimSpace(strings.TrimPrefix(line, "module"))
			continue
		}

		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}

		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		if inRequire {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				depPath := parts[0]
				depVer := parts[1]
				// Remove potential comments
				if idx := strings.Index(depVer, "//"); idx != -1 {
					depVer = strings.TrimSpace(depVer[:idx])
				}
				info.Dependencies[depPath] = depVer
			}
		} else if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				depPath := parts[1]
				depVer := parts[2]
				if idx := strings.Index(depVer, "//"); idx != -1 {
					depVer = strings.TrimSpace(depVer[:idx])
				}
				info.Dependencies[depPath] = depVer
			}
		}
	}

	return info, nil
}

// MatchVersion validates if the resolved version of a dependency matches a semver constraint.
func MatchVersion(resolvedVer, constraintStr string) bool {
	if resolvedVer == "" {
		return false
	}
	// Strip "v" prefix if present for parsing
	resolvedVer = strings.TrimPrefix(resolvedVer, "v")

	c, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return false
	}

	v, err := semver.NewVersion(resolvedVer)
	if err != nil {
		// If resolvedVer is not a valid semver (e.g. master branch or pseudo-version),
		// fall back to simple string prefix/containment matching.
		return strings.Contains(resolvedVer, constraintStr)
	}

	return c.Check(v)
}

// GetActiveDepsFromEnv parses active dependency mappings from the OTELC_ACTIVE_DEPS env var.
func GetActiveDepsFromEnv() map[string]string {
	deps := make(map[string]string)
	env := os.Getenv("OTELC_ACTIVE_DEPS")
	if env == "" {
		return deps
	}
	pairs := strings.Split(env, ";")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			deps[parts[0]] = parts[1]
		}
	}
	return deps
}
