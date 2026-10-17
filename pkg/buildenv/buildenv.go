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

package buildenv

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"
)

const (
	AnnotationCreated         = "org.opencontainers.image.created"
	AnnotationURL             = "org.opencontainers.image.url"
	AnnotationSource          = "org.opencontainers.image.source"
	AnnotationVersion         = "org.opencontainers.image.version"
	AnnotationRevision        = "org.opencontainers.image.revision"
	AnnotationBaseImageName   = "org.opencontainers.image.base.name"
	AnnotationBaseImageDigest = "org.opencontainers.image.base.digest"
)

type Source struct {
	URL      string
	Revision string
	Version  string
}

func GitSource(dir, cloneURL string) Source {
	gitDir, err := findGitDir(dir)
	if err != nil {
		logrus.Debugf("Not reading the source of the build context from git: %v", err)
		return Source{}
	}
	// a worktree keeps HEAD in its own git dir and the refs and config in the common one
	commonDir := gitDir
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err == nil {
		commonDir = resolve(gitDir, strings.TrimSpace(string(data)))
	}
	head, err := checkedOutCommit(gitDir, commonDir)
	if err != nil {
		logrus.Warnf("Not reading the source of the build context from git, resolving HEAD: %v", err)
		return Source{}
	}
	url := cloneURL
	if url == "" {
		url = originURL(commonDir)
	}
	return Source{URL: withoutCredentials(url), Revision: head}
}

// The CI environment describes the pipeline, which is only the build context if the
// context is checked out at the pipeline's commit. A job can check out another commit,
// clone another repository or build in a submodule.
func ResolveSource(ci CI, git Source) Source {
	if ci.Forge == "" {
		return git
	}
	if git.Revision != "" && git.Revision != ci.Checkout.Revision {
		logrus.Warnf("The build context is checked out at %s, not at the pipeline's commit %s, annotating the checkout", git.Revision, ci.Checkout.Revision)
		return git
	}
	return ci.Checkout
}

// distribution/distribution spells Docker Hub docker.io, go-containerregistry index.docker.io
func QualifiedReference(ref string) string {
	parsed, err := name.ParseReference(ref, name.WeakValidation)
	if err != nil {
		return ref
	}
	qualified := parsed.Name()
	if parsed.Context().RegistryStr() == name.DefaultRegistry {
		qualified = "docker.io" + strings.TrimPrefix(qualified, name.DefaultRegistry)
	}
	return qualified
}

func Annotations(src Source) map[string]string {
	out := map[string]string{}
	if src.URL != "" {
		out[AnnotationSource] = src.URL
		out[AnnotationURL] = src.URL
	}
	if src.Revision != "" {
		out[AnnotationRevision] = src.Revision
	}
	if src.Version != "" {
		out[AnnotationVersion] = src.Version
	}
	return out
}

func withoutCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}

func originURL(commonDir string) string {
	data, err := os.ReadFile(filepath.Join(commonDir, "config"))
	if err != nil {
		return ""
	}
	inOrigin := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
		} else if inOrigin {
			key, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == "url" {
				return scpToURL(strings.TrimSpace(value))
			}
		}
	}
	return ""
}

// git reads user@host:path as ssh when there is no scheme and no slash before the colon
func scpToURL(remote string) string {
	host, path, found := strings.Cut(remote, ":")
	if !found || strings.Contains(remote, "://") || strings.Contains(host, "/") {
		return remote
	}
	return "ssh://" + host + "/" + strings.TrimPrefix(path, "/")
}

func checkedOutCommit(gitDir, commonDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	ref, symbolic := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: ")
	if !symbolic {
		return ref, nil
	}
	data, err = os.ReadFile(filepath.Join(commonDir, ref))
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	packed, err := os.ReadFile(filepath.Join(commonDir, "packed-refs"))
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	for line := range strings.SplitSeq(string(packed), "\n") {
		sha, name, ok := strings.Cut(line, " ")
		if ok && name == ref {
			return sha, nil
		}
	}
	return "", fmt.Errorf("resolving %s: not found", ref)
}

func findGitDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		dotGit := filepath.Join(dir, ".git")
		fi, err := os.Stat(dotGit)
		switch {
		case err == nil && fi.IsDir():
			return dotGit, nil
		case err == nil:
			data, err := os.ReadFile(dotGit)
			if err != nil {
				return "", err
			}
			gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
			if !ok {
				return "", fmt.Errorf("%s is not a gitdir file", dotGit)
			}
			return resolve(dir, gitDir), nil
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a git repository")
		}
		dir = parent
	}
}

func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
