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

package policy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/v1/rego"
)

// Input is one Docker-style build input presented to a Rego policy.
type Input struct {
	Local bool        `json:"local,omitempty"`
	Image *ImageInput `json:"image,omitempty"`
	Git   *GitInput   `json:"git,omitempty"`
	HTTP  *HTTPInput  `json:"http,omitempty"`
}

// ImageInput describes an image reference and the resolved image metadata.
type ImageInput struct {
	Ref           string `json:"ref"`
	ResolvedRef   string `json:"resolvedRef"`
	IsCanonical   bool   `json:"isCanonical"`
	Digest        string `json:"digest"`
	Checksum      string `json:"checksum"`
	Host          string `json:"host"`
	Repo          string `json:"repo"`
	FullRepo      string `json:"fullRepo"`
	Tag           string `json:"tag"`
	Platform      string `json:"platform"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	HasProvenance bool   `json:"hasProvenance"`
	Attestations  []any  `json:"attestations"`
	Signatures    []any  `json:"signatures"`
}

// GitInput describes a Git repository and its resolved reference.
type GitInput struct {
	URL            string `json:"url"`
	Remote         string `json:"remote"`
	Schema         string `json:"schema"`
	Host           string `json:"host"`
	Ref            string `json:"ref"`
	Commit         string `json:"commit"`
	Checksum       string `json:"checksum"`
	CommitChecksum string `json:"commitChecksum"`
	IsTag          bool   `json:"isTag"`
	IsBranch       bool   `json:"isBranch"`
	IsCommitRef    bool   `json:"isCommitRef"`
	TagName        string `json:"tagName"`
	Branch         string `json:"branch"`
	IsAnnotatedTag bool   `json:"isAnnotatedTag"`
	SignedTag      bool   `json:"signedTag"`
	SignedCommit   bool   `json:"signedCommit"`
}

// HTTPInput describes a remote HTTP artifact.
type HTTPInput struct {
	URL       string `json:"url"`
	Schema    string `json:"schema"`
	Host      string `json:"host"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	Checksum  string `json:"checksum"`
	MediaType string `json:"mediaType"`
	HasAuth   bool   `json:"hasAuth"`
}

// Policy is a compiled Docker input policy.
type Policy struct {
	prepared rego.PreparedEvalQuery
}

// NewHTTPInput parses an HTTP source and removes URL credentials from policy input.
func NewHTTPInput(rawURL, filename, checksum, mediaType string) (HTTPInput, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return HTTPInput{}, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return HTTPInput{}, fmt.Errorf("URL %q must include a scheme and host", rawURL)
	}
	if filename == "" {
		filename = path.Base(strings.TrimRight(parsed.Path, "/"))
		if filename == "." {
			filename = ""
		}
	}
	hasAuth := parsed.User != nil
	parsed.User = nil
	return HTTPInput{
		URL:       parsed.String(),
		Schema:    parsed.Scheme,
		Host:      parsed.Hostname(),
		Path:      parsed.RequestURI(),
		Filename:  filename,
		Checksum:  checksum,
		MediaType: mediaType,
		HasAuth:   hasAuth,
	}, nil
}

// Load compiles a policy file, returning nil when the file does not exist.
func Load(path string) (*Policy, error) {
	module, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading input policy %q: %w", path, err)
	}

	prepared, err := rego.New(
		rego.Query("data.docker.decision"),
		rego.Module(filepath.Base(path), string(module)),
	).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("compiling input policy %q: %w", path, err)
	}
	return &Policy{prepared: prepared}, nil
}

// Evaluate checks one input against the compiled policy.
func (p *Policy) Evaluate(input Input) error {
	if p == nil {
		return nil
	}
	results, err := p.prepared.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		return fmt.Errorf("evaluating input policy: %w", err)
	}
	if len(results) != 1 || len(results[0].Expressions) != 1 {
		return errors.New("input policy did not produce data.docker.decision")
	}
	value, ok := results[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return fmt.Errorf("input policy decision must be an object, got %T", results[0].Expressions[0].Value)
	}
	allowed, ok := value["allow"].(bool)
	if !ok {
		return errors.New("input policy decision must include a boolean allow field")
	}
	if allowed {
		return nil
	}

	messages := decisionMessages(value["deny_msg"])
	if len(messages) == 0 {
		messages = append(messages, "decision allow=false")
	}
	return fmt.Errorf("%s input denied by policy (decision allow=false): %s", describe(input), strings.Join(messages, "; "))
}

func decisionMessages(value any) []string {
	switch messages := value.(type) {
	case string:
		if messages != "" {
			return []string{messages}
		}
	case []any:
		out := make([]string, 0, len(messages))
		for _, message := range messages {
			if text, ok := message.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func describe(input Input) string {
	switch {
	case input.Image != nil:
		return fmt.Sprintf("image %q", input.Image.Ref)
	case input.Git != nil:
		return fmt.Sprintf("git source %q", safeURL(input.Git.Remote))
	case input.HTTP != nil:
		return fmt.Sprintf("HTTP source %q", safeURL(input.HTTP.URL))
	case input.Local:
		return "local"
	default:
		return "unknown input"
	}
}

func safeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}
