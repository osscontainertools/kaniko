/*
Copyright 2018 Google LLC

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

package executor

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
)

const emptyState = "0000000000000000000000000000000000000000000000000000000000000000"

// NewCompositeCache returns an initialized composite cache object.
func NewCompositeCache(initial ...string) *CompositeCache {
	c := CompositeCache{}
	c.AddKey(initial...)
	return &c
}

func ResumeCompositeCache(state string) *CompositeCache {
	if !config.FF.RollingCacheKey {
		return NewCompositeCache(state)
	}
	return &CompositeCache{state: state, keys: []string{state}}
}

// CompositeCache is a type that generates a cache key from a series of keys.
type CompositeCache struct {
	state string
	keys  []string
}

// Clone returns an independent copy of the CompositeCache with its own backing array.
func (s CompositeCache) Clone() CompositeCache {
	return CompositeCache{state: s.state, keys: append([]string(nil), s.keys...)}
}

// AddKey adds the specified key to the sequence.
func (s *CompositeCache) AddKey(k ...string) {
	if config.FF.RollingCacheKey {
		for _, key := range k {
			state := s.state
			if state == "" {
				state = emptyState
			}
			digest := sha256.Sum256([]byte(state + key))
			s.state = hex.EncodeToString(digest[:])
		}
	}
	s.keys = append(s.keys, k...)
}

// Key returns the human readable composite key as a string.
func (s *CompositeCache) Key() string {
	return strings.Join(s.keys, "-")
}

// State returns the representation that ResumeCompositeCache resumes from.
func (s *CompositeCache) State() string {
	if !config.FF.RollingCacheKey {
		return s.Key()
	}
	if s.state == "" {
		return emptyState
	}
	return s.state
}

// Hash returns the composite key in a string SHA256 format.
func (s *CompositeCache) Hash() (string, error) {
	if !config.FF.RollingCacheKey {
		return util.SHA256(strings.NewReader(s.Key()))
	}
	if s.state == "" {
		return emptyState, nil
	}
	return s.state, nil
}

func (s *CompositeCache) AddPath(p string, context util.FileContext) error {
	sha := sha256.New()
	fi, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("could not add path: %w", err)
	}

	if fi.Mode().IsDir() {
		empty, k, err := hashDir(p, context)
		if err != nil {
			return err
		}

		// Only add the hash of this directory to the key
		// if there is any ignored content.
		if !empty || !context.ExcludesFile(p) {
			s.AddKey(k)
		}
		return nil
	}

	if context.ExcludesFile(p) {
		return nil
	}
	fh, err := util.CacheHasher()(p)
	if err != nil {
		return err
	}
	if _, err := sha.Write([]byte(fh)); err != nil {
		return err
	}

	s.AddKey(hex.EncodeToString(sha.Sum(nil)))
	return nil
}

// HashDir returns a hash of the directory.
func hashDir(p string, context util.FileContext) (bool, string, error) {
	sha := sha256.New()
	framed := config.FF.HashDirFraming
	empty := true
	if err := fs.WalkDir(util.FSys, p, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		exclude := context.ExcludesFile(path)
		if exclude {
			return nil
		}

		fileHash, err := util.CacheHasher()(path)
		if err != nil {
			return err
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		absRoot, err := filepath.Abs(context.Root)
		if err != nil {
			return err
		}

		if err := writeDirHashEntry(sha, strings.TrimPrefix(absPath, absRoot), fileHash, framed); err != nil {
			return err
		}
		empty = false
		return nil
	}); err != nil {
		return false, "", err
	}

	return empty, hex.EncodeToString(sha.Sum(nil)), nil
}

func writeDirHashEntry(sha hash.Hash, path, fileHash string, framed bool) error {
	if !framed {
		if _, err := sha.Write([]byte(path)); err != nil {
			return err
		}
		_, err := sha.Write([]byte(fileHash))
		return err
	}

	if err := writeLengthPrefixedHashField(sha, path); err != nil {
		return err
	}
	return writeLengthPrefixedHashField(sha, fileHash)
}

func writeLengthPrefixedHashField(sha hash.Hash, value string) error {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	if _, err := sha.Write(length[:]); err != nil {
		return err
	}
	_, err := sha.Write([]byte(value))
	return err
}
