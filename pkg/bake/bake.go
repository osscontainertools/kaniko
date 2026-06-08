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
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Target struct {
	Target      string   `json:"target"`
	Destination []string `json:"destination"`
}

type Bakefile struct {
	Version string            `json:"version"`
	Targets map[string]Target `json:"targets"`
}

type ResolvedTarget struct {
	ID          string
	Stage       string
	Destination []string
}

func Parse(path string) (*Bakefile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bakefile: %w", err)
	}
	return parse(data)
}

func parse(data []byte) (*Bakefile, error) {
	b := &Bakefile{}
	if err := json.Unmarshal(data, b); err != nil {
		return nil, fmt.Errorf("parsing bakefile: %w", err)
	}
	if b.Version != "1" {
		return nil, fmt.Errorf("unsupported bakefile version %q, expected %q", b.Version, "1")
	}
	if len(b.Targets) == 0 {
		return nil, errors.New("bakefile defines no targets")
	}
	return b, nil
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
		resolved = append(resolved, ResolvedTarget{ID: id, Stage: stage, Destination: t.Destination})
	}
	return resolved, nil
}

type Override struct {
	Target string
	Field  string
	Value  string
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
	for _, o := range overrides {
		i, ok := idx[o.Target]
		if !ok {
			return fmt.Errorf("--set target %q is not built", o.Target)
		}
		switch o.Field {
		case "destination":
			dests[i] = append(dests[i], o.Value)
		default:
			return fmt.Errorf("--set field %q is not supported", o.Field)
		}
	}
	for i, d := range dests {
		targets[i].Destination = d
	}
	return nil
}
