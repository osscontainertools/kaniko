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
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/hcl/hcl/token"
)

type Target struct {
	Target      string   `hcl:"target"`
	Destination []string `hcl:"destination"`
}

type Bakefile struct {
	Version string            `hcl:"version"`
	Targets map[string]Target `hcl:"target"`
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
	b, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

func parse(data []byte) (*Bakefile, error) {
	f, err := hcl.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing bakefile: %w", err)
	}
	list, ok := f.Node.(*ast.ObjectList)
	if !ok {
		return nil, errors.New("bakefile must be a list of assignments and target blocks")
	}
	if err := validate(list); err != nil {
		return nil, err
	}
	b := &Bakefile{}
	if err := hcl.DecodeObject(b, f.Node); err != nil {
		return nil, fmt.Errorf("decoding bakefile: %w", err)
	}
	if b.Version != "1" {
		return nil, fmt.Errorf("unsupported bakefile version %q, expected %q", b.Version, "1")
	}
	if len(b.Targets) == 0 {
		return nil, errors.New("bakefile defines no targets")
	}
	return b, nil
}

var (
	topKeys    = []string{"version", "target"}
	targetKeys = []string{"target", "destination"}
)

// Keys a docker-bake.hcl would use. The bakefile keeps kaniko's own vocabulary, so
// point at the kaniko spelling rather than accepting both.
var bakeHints = map[string]string{
	"tags":       "use destination",
	"context":    "use the --context flag",
	"dockerfile": "use the --dockerfile flag",
	"args":       "use the --build-arg flag",
	"platforms":  "use the --custom-platform flag",
	"inherits":   "which is not supported",
}

func validate(list *ast.ObjectList) error {
	seen := map[string]token.Pos{}
	for _, item := range list.Items {
		if len(item.Keys) == 0 {
			return fmt.Errorf("%s: unnamed block", item.Pos())
		}
		name := keyName(item.Keys[0])
		if !slices.Contains(topKeys, name) {
			return fmt.Errorf("%s: unsupported key %q, expected one of %s", item.Pos(), name, strings.Join(topKeys, ", "))
		}
		if name != "target" {
			err := checkPortable(name, item)
			if err != nil {
				return err
			}
			continue
		}
		if len(item.Keys) != 2 {
			return fmt.Errorf("%s: target takes exactly one name, as in `target \"app\" { ... }`", item.Pos())
		}
		label := keyName(item.Keys[1])
		prev, dup := seen[label]
		if dup {
			return fmt.Errorf("%s: duplicate target %q, first defined at %s", item.Pos(), label, prev)
		}
		seen[label] = item.Pos()
		body, ok := item.Val.(*ast.ObjectType)
		if !ok {
			return fmt.Errorf("%s: target %q must be a block", item.Pos(), label)
		}
		err := validateTarget(label, body.List)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateTarget(label string, list *ast.ObjectList) error {
	for _, item := range list.Items {
		if len(item.Keys) == 0 {
			return fmt.Errorf("%s: unnamed key in target %q", item.Pos(), label)
		}
		key := keyName(item.Keys[0])
		if !slices.Contains(targetKeys, key) {
			hint, known := bakeHints[key]
			if known {
				return fmt.Errorf("%s: %q in target %q is a docker-bake.hcl key, %s", item.Pos(), key, label, hint)
			}
			return fmt.Errorf("%s: unsupported key %q in target %q, expected one of %s", item.Pos(), key, label, strings.Join(targetKeys, ", "))
		}
		if len(item.Keys) > 1 {
			return fmt.Errorf("%s: key %q in target %q takes no name", item.Pos(), key, label)
		}
		err := checkPortable(key, item)
		if err != nil {
			return err
		}
	}
	return nil
}

// checkPortable rejects the two constructs hcl1 accepts but reads differently from
// hcl2, so bakefiles written today keep their meaning if the parser is ever upgraded.
// hcl1 takes both `args { ... }` and `args = { ... }` for a map, hcl2 takes only the
// second, and hcl1 leaves "${VAR}" as a literal where hcl2 interpolates it.
func checkPortable(key string, item *ast.ObjectItem) error {
	_, isBlock := item.Val.(*ast.ObjectType)
	if isBlock && !item.Assign.IsValid() {
		return fmt.Errorf("%s: %q must use `=`, as in `%s = { ... }`", item.Pos(), key, key)
	}
	var bad error
	ast.Walk(item.Val, func(n ast.Node) (ast.Node, bool) {
		lit, ok := n.(*ast.LiteralType)
		if !ok || lit.Token.Type != token.STRING {
			return n, true
		}
		if strings.Contains(lit.Token.Text, "${") {
			bad = fmt.Errorf("%s: variables are not supported, found %s", lit.Pos(), lit.Token.Text)
			return n, false
		}
		return n, true
	})
	return bad
}

func keyName(k *ast.ObjectKey) string {
	return strings.Trim(k.Token.Text, `"`)
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
