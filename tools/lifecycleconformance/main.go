// Command lifecycleconformance emits a review-only comparison, never runtime data.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go.yaml.in/yaml/v3"
	"os"
	"sort"
	"strings"
)

type officialEntry struct{ Group, Version, Kind, Introduced, Deprecated, Removed, Replacement string }
type plutoEntry struct {
	Version     string `yaml:"version"`
	Kind        string `yaml:"kind"`
	Deprecated  string `yaml:"deprecated-in"`
	Removed     string `yaml:"removed-in"`
	Replacement string `yaml:"replacement-api"`
	Available   string `yaml:"replacement-available-in"`
	Component   string `yaml:"component"`
}
type comparison struct {
	GVK                           string
	Official                      *officialEntry
	Pluto                         *plutoEntry
	Differences                   []string
	OfficialReplacementIntroduced string
}

func compare(official []byte, pluto []byte) ([]comparison, error) {
	var catalog struct{ Entries []officialEntry }
	if err := json.Unmarshal(official, &catalog); err != nil {
		return nil, err
	}
	var ref struct {
		Entries []plutoEntry `yaml:"deprecated-versions"`
	}
	if err := yaml.Unmarshal(pluto, &ref); err != nil {
		return nil, err
	}
	selected := map[string]bool{"batch/v1beta1/CronJob": true, "extensions/v1beta1/Deployment": true, "extensions/v1beta1/DeploymentRollback": true, "extensions/v1beta1/Ingress": true, "policy/v1beta1/PodSecurityPolicy": true}
	all := map[string]*comparison{}
	for key := range selected {
		all[key] = &comparison{GVK: key}
	}
	introduced := make(map[string]string, len(catalog.Entries))
	for _, e := range catalog.Entries {
		key := e.Group + "/" + e.Version + "/" + e.Kind
		introduced[key] = e.Introduced
		if c := all[key]; c != nil {
			copy := e
			c.Official = &copy
		}
	}
	for _, e := range ref.Entries {
		key := e.Version + "/" + e.Kind
		if c := all[key]; c != nil && e.Component == "k8s" {
			copy := e
			c.Pluto = &copy
		}
	}
	normalize := func(s string) string { s = strings.TrimPrefix(s, "v"); return strings.TrimSuffix(s, ".0") }
	out := make([]comparison, 0, len(all))
	for _, c := range all {
		c.OfficialReplacementIntroduced = "unknown"
		if c.Official != nil {
			if c.Official.Replacement == "" {
				c.OfficialReplacementIntroduced = "not-applicable"
			} else if intro := introduced[strings.Replace(c.Official.Replacement, " ", "/", 1)]; intro != "" {
				c.OfficialReplacementIntroduced = normalize(intro)
			}
		}
		if c.OfficialReplacementIntroduced == "unknown" {
			c.Differences = append(c.Differences, "official replacement introduction absent: UNKNOWN")
		}
		if c.Pluto != nil && c.Pluto.Replacement != "" {
			if c.Pluto.Available == "" {
				c.Differences = append(c.Differences, "Pluto replacement availability absent")
			} else if c.OfficialReplacementIntroduced != "unknown" && c.OfficialReplacementIntroduced != normalize(c.Pluto.Available) {
				c.Differences = append(c.Differences, "replacement-available-in: official="+c.OfficialReplacementIntroduced+" Pluto="+normalize(c.Pluto.Available))
			}
		}
		if c.Official == nil {
			c.Differences = append(c.Differences, "official coverage absent: UNKNOWN; no override")
		}
		if c.Pluto == nil {
			c.Differences = append(c.Differences, "Pluto exact-GVK coverage absent")
		}
		if c.Official != nil && c.Pluto != nil {
			for _, field := range []struct{ name, left, right string }{{"deprecated", c.Official.Deprecated, normalize(c.Pluto.Deprecated)}, {"removed", c.Official.Removed, normalize(c.Pluto.Removed)}} {
				if field.left != field.right {
					c.Differences = append(c.Differences, field.name+": official="+field.left+" Pluto="+field.right)
				}
			}
			replacement := strings.TrimSuffix(c.Official.Replacement, " "+c.Official.Kind)
			if replacement != c.Pluto.Replacement {
				c.Differences = append(c.Differences, "replacement: official="+replacement+" Pluto="+c.Pluto.Replacement)
			}
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GVK < out[j].GVK })
	return out, nil
}

func run(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: lifecycleconformance PINNED_PLUTO_VERSIONS_YAML PINNED_KUBEPUG_GEN_GO (run from repository root)")
	}
	fixture, err := os.ReadFile("internal/readiness/testdata/lifecycle-conformance.json")
	if err != nil {
		return err
	}
	var pins struct{ OfficialDigest, PlutoDigest, KubepugDigest string }
	if err = json.Unmarshal(fixture, &pins); err != nil {
		return err
	}
	inputs := []struct{ path, digest string }{{"data/api-lifecycle.json", pins.OfficialDigest}, {args[0], pins.PlutoDigest}, {args[1], pins.KubepugDigest}}
	payloads := make([][]byte, len(inputs))
	for i, input := range inputs {
		raw, err := os.ReadFile(input.path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != input.digest {
			return fmt.Errorf("pinned digest mismatch: %s; review source revision before regenerating", input.path)
		}
		payloads[i] = raw
	}
	// Kubepug has no comparable dataset: assert only the pinned generator input, not output equality.
	result, err := compare(payloads[0], payloads[1])
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
