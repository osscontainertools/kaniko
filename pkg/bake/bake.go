/*
Copyright 2026 OSS Container Tools

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bake

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Target struct {
	Name           string   `hcl:"name,label"`
	Context        string   `hcl:"context,optional"`
	ContextSubPath string   `hcl:"context_sub_path,optional"`
	Dockerfile     string   `hcl:"dockerfile,optional"`
	Target         string   `hcl:"target,optional"`
	Destination    []string `hcl:"destination,optional"`
	CacheFrom      []string `hcl:"cache_from,optional"`
	CacheTo        []string `hcl:"cache_to,optional"`
}

type Bakefile struct {
	Dir     string
	Targets map[string]Target
}

type ResolvedTarget struct {
	ID             string   `json:"-"`
	Context        string   `json:"context,omitempty"`
	ContextSubPath string   `json:"context_sub_path,omitempty"`
	Dockerfile     string   `json:"dockerfile,omitempty"`
	Stage          string   `json:"target,omitempty"`
	Destination    []string `json:"destination,omitempty"`
	CacheFrom      []string `json:"cache_from,omitempty"`
	CacheTo        []string `json:"cache_to,omitempty"`
}

func Parse(path string) (*Bakefile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bakefile: %w", err)
	}
	f, diags := hclsyntax.ParseConfig(data, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("%s: bakefile is not in HCL native syntax", path)
	}
	diags = checkTargets(body)
	if diags.HasErrors() {
		return nil, diags
	}
	var decoded struct {
		Targets []Target `hcl:"target,block"`
	}
	diags = gohcl.DecodeBody(body, nil, &decoded)
	if diags.HasErrors() {
		return nil, diags
	}
	if len(decoded.Targets) == 0 {
		return nil, fmt.Errorf("%s: bakefile defines no targets", path)
	}
	b := &Bakefile{Dir: filepath.Dir(path), Targets: map[string]Target{}}
	for _, t := range decoded.Targets {
		b.Targets[t.Name] = t
	}
	return b, nil
}

// Keys a docker-bake.hcl would use. The bakefile keeps kaniko's own vocabulary, so
// point at the kaniko spelling rather than accepting both.
var bakeHints = map[string]string{
	"tags":       "use destination",
	"args":       "use the --build-arg flag",
	"platforms":  "use the --custom-platform flag",
	"inherits":   "which is not supported",
	"cache-from": "use cache_from",
	"cache-to":   "use cache_to",
}

func checkTargets(body *hclsyntax.Body) hcl.Diagnostics {
	var diags hcl.Diagnostics
	seen := map[string]hcl.Range{}
	for _, block := range body.Blocks {
		if block.Type != "target" || len(block.Labels) != 1 {
			continue
		}
		name := block.Labels[0]
		prev, dup := seen[name]
		if dup {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Duplicate target",
				Detail:   fmt.Sprintf("Target %q is already defined at %s.", name, prev),
				Subject:  block.DefRange().Ptr(),
			})
		}
		seen[name] = block.DefRange()
		attrs := slices.SortedFunc(maps.Values(block.Body.Attributes), func(a, b *hclsyntax.Attribute) int {
			return a.NameRange.Start.Byte - b.NameRange.Start.Byte
		})
		for _, attr := range attrs {
			hint, known := bakeHints[attr.Name]
			if known {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Unsupported argument",
					Detail:   fmt.Sprintf("%q is a docker-bake.hcl key, %s.", attr.Name, hint),
					Subject:  attr.NameRange.Ptr(),
				})
			}
		}
	}
	return diags
}

func Load(path string, selection, set []string) ([]ResolvedTarget, error) {
	bakefile, err := Parse(path)
	if err != nil {
		return nil, err
	}
	targets, err := bakefile.Resolve(selection)
	if err != nil {
		return nil, err
	}
	overrides := make([]Override, 0, len(set))
	for _, s := range set {
		o, err := ParseOverride(s)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	if err := ApplyOverrides(targets, overrides); err != nil {
		return nil, err
	}
	for i := range targets {
		t := &targets[i]
		remote := strings.Contains(t.Context, "://")
		if t.Context != "" && !filepath.IsAbs(t.Context) && !remote {
			t.Context = filepath.Join(bakefile.Dir, t.Context)
		}
		// A relative dockerfile in a remote context is joined once the context is unpacked.
		if t.Dockerfile != "" && !filepath.IsAbs(t.Dockerfile) && !strings.Contains(t.Dockerfile, "://") && !remote {
			if t.Context != "" {
				t.Dockerfile = filepath.Join(t.Context, t.Dockerfile)
			} else {
				t.Dockerfile = filepath.Join(bakefile.Dir, t.Dockerfile)
			}
		}
	}
	return targets, nil
}

func (b *Bakefile) Resolve(selected []string) ([]ResolvedTarget, error) {
	ids := selected
	if len(ids) == 0 {
		ids = make([]string, 0, len(b.Targets))
		for id := range b.Targets {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	resolved := make([]ResolvedTarget, 0, len(ids))
	for _, id := range ids {
		t, ok := b.Targets[id]
		if !ok {
			return nil, fmt.Errorf("unknown target %q", id)
		}
		stage := t.Target
		if stage == "" {
			stage = id
		}
		resolved = append(resolved, ResolvedTarget{
			ID:             id,
			Context:        t.Context,
			ContextSubPath: t.ContextSubPath,
			Dockerfile:     t.Dockerfile,
			Stage:          stage,
			Destination:    t.Destination,
			CacheFrom:      t.CacheFrom,
			CacheTo:        t.CacheTo,
		})
	}
	return resolved, nil
}

type Override struct {
	Target string
	Field  string
	Value  string
}

func Print(targets []ResolvedTarget) ([]byte, error) {
	byID := make(map[string]ResolvedTarget, len(targets))
	for _, t := range targets {
		byID[t.ID] = t
	}
	return json.MarshalIndent(map[string]any{"target": byID}, "", "  ")
}

func ParseOverride(s string) (Override, error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok {
		return Override{}, fmt.Errorf("invalid --set %q, want <target>.<field>=<value>", s)
	}
	target, field, ok := strings.Cut(key, ".")
	if !ok || target == "" || field == "" {
		return Override{}, fmt.Errorf("invalid --set %q, want <target>.<field>=<value>", s)
	}
	return Override{Target: target, Field: field, Value: value}, nil
}

func ApplyOverrides(targets []ResolvedTarget, overrides []Override) error {
	idx := make(map[string]int, len(targets))
	for i, t := range targets {
		idx[t.ID] = i
	}
	dests := map[int][]string{}
	froms := map[int][]string{}
	tos := map[int][]string{}
	contexts := map[int]string{}
	contextSubPaths := map[int]string{}
	dockerfiles := map[int]string{}
	for _, o := range overrides {
		i, ok := idx[o.Target]
		if !ok {
			return fmt.Errorf("--set target %q is not built", o.Target)
		}
		switch o.Field {
		case "destination":
			dests[i] = append(dests[i], o.Value)
		case "cache_from":
			froms[i] = append(froms[i], o.Value)
		case "cache_to":
			tos[i] = append(tos[i], o.Value)
		case "context":
			contexts[i] = o.Value
		case "context_sub_path":
			contextSubPaths[i] = o.Value
		case "dockerfile":
			dockerfiles[i] = o.Value
		default:
			return fmt.Errorf("--set field %q is not supported", o.Field)
		}
	}
	for i, d := range dests {
		targets[i].Destination = d
	}
	for i, f := range froms {
		targets[i].CacheFrom = f
	}
	for i, t := range tos {
		targets[i].CacheTo = t
	}
	for i, c := range contexts {
		targets[i].Context = c
	}
	for i, p := range contextSubPaths {
		targets[i].ContextSubPath = p
	}
	for i, df := range dockerfiles {
		targets[i].Dockerfile = df
	}
	return nil
}
