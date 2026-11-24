package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// NestedMountpoint is a volume or tmpfs mount whose target sits inside a bind
// mount, such as a node_modules volume at /app/node_modules over a checkout
// bound at /app. The Docker daemon has to create the mountpoint directory
// inside the bind source, on the host, and it does so as root when the
// directory is missing. HostPath is where that directory lands on the host.
type NestedMountpoint struct {
	Service  string `json:"service"`
	Target   string `json:"target"`
	HostPath string `json:"host_path"`
}

type composeConfig struct {
	Services map[string]struct {
		Volumes []struct {
			Type   string `json:"type"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"volumes"`
	} `json:"services"`
}

// ParseNestedMountpoints reads `docker compose config --format json` output
// and returns every mount nested inside one of the same service's bind
// mounts, sorted by service and target. When binds nest inside each other,
// the deepest bind that contains the mount decides its host path.
func ParseNestedMountpoints(configJSON []byte) ([]NestedMountpoint, error) {
	var cfg composeConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("parsing compose config: %w", err)
	}

	var result []NestedMountpoint
	for name, svc := range cfg.Services {
		for _, m := range svc.Volumes {
			if m.Type == "bind" {
				continue
			}
			var bestSource, bestTarget string
			for _, b := range svc.Volumes {
				if b.Type != "bind" || !within(m.Target, b.Target) {
					continue
				}
				if len(b.Target) > len(bestTarget) {
					bestSource, bestTarget = b.Source, b.Target
				}
			}
			if bestTarget == "" {
				continue
			}
			rel := strings.TrimPrefix(m.Target, strings.TrimSuffix(bestTarget, "/")+"/")
			result = append(result, NestedMountpoint{
				Service:  name,
				Target:   m.Target,
				HostPath: filepath.Join(bestSource, filepath.FromSlash(rel)),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Service != result[j].Service {
			return result[i].Service < result[j].Service
		}
		return result[i].Target < result[j].Target
	})
	return result, nil
}

// within reports whether target lies strictly inside dir (container paths).
func within(target, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	return strings.HasPrefix(target, dir+"/") && len(target) > len(dir)+1
}

// NestedMountpoints returns the nested mountpoints of every service in the
// compose file, across all of its profiles.
func (c *Compose) NestedMountpoints(ctx context.Context) ([]NestedMountpoint, error) {
	out, err := c.command(ctx, "--profile", "*", "config", "--format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose config: %w", err)
	}
	return ParseNestedMountpoints(out)
}
