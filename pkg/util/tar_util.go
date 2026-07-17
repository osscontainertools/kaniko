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

package util

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/moby/go-archive"
	"github.com/moby/go-archive/compression"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/sirupsen/logrus"
)

// Tar knows how to write files to a tar file.
type Tar struct {
	hardlinks map[uint64]string
	seen      map[string]struct{}
	w         *tar.Writer
}

// NewTar will create an instance of Tar that can write files to the writer at f.
func NewTar(f io.Writer) Tar {
	w := tar.NewWriter(f)
	return Tar{
		w:         w,
		hardlinks: map[uint64]string{},
		seen:      map[string]struct{}{},
	}
}

// entryName normalizes a filesystem path to the header name used in the layer
// tar, without a trailing slash so files and directories share one key space.
func entryName(p string) string {
	name := strings.TrimPrefix(p, config.RootDir)
	name = strings.TrimLeft(name, "/")
	return strings.TrimSuffix(name, "/")
}

// assertEntry checks the layer-shape invariants for a header name about to be
// written and records it as seen.
func (t *Tar) assertEntry(name string) {
	// A duplicate entry makes extraction order-dependent and is always a
	// snapshot bookkeeping bug.
	_, dup := t.seen[name]
	assert.Assert("tar.entry-unique", !dup, "tar entry %q must be written at most once", name)
	// Entry names must stay relative and inside the image root.
	clean := name != "" && !strings.HasPrefix(name, "/") && !strings.Contains("/"+name+"/", "/../")
	assert.Assert("tar.name-clean", clean, "tar entry name %q must be a clean relative path", name)
	// The executor's own directory must never leak into an image layer; this is
	// the last line of defense after the ignore list.
	kdir := strings.TrimLeft(config.KanikoDir, "/")
	assert.Assert("tar.kaniko-excluded", name != kdir && !strings.HasPrefix(name, kdir+"/"), "tar entry %q must not be inside the kaniko directory", name)
	t.seen[name] = struct{}{}
}

// Close will close any open streams used by Tar.
func (t *Tar) Close() {
	t.w.Close()
}

// AddFileToTar adds the file at path p to the tar
func (t *Tar) AddFileToTar(p string) error {
	i, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("failed to get file info for %s: %w", p, err)
	}
	linkDst := ""
	if i.Mode()&os.ModeSymlink != 0 {
		var err error
		linkDst, err = os.Readlink(p)
		if err != nil {
			return err
		}
	}
	if i.Mode()&os.ModeSocket != 0 {
		logrus.Infof("Ignoring socket %s, not adding to tar", i.Name())
		return nil
	}
	hdr, err := tar.FileInfoHeader(i, linkDst)
	if err != nil {
		return err
	}
	err = readSecurityXattrToTarHeader(p, hdr)
	if err != nil {
		return err
	}

	assert.Assert("tar.root-path-excluded", p != config.RootDir, "snapshot must not include root path '/'")

	// Docker uses no leading / in the tarball
	hdr.Name = strings.TrimPrefix(p, config.RootDir)
	hdr.Name = strings.TrimLeft(hdr.Name, "/")

	if hdr.Typeflag == tar.TypeDir && !strings.HasSuffix(hdr.Name, "/") {
		hdr.Name = hdr.Name + "/"
	}
	// rootfs may not have been extracted when using cache, preventing uname/gname from resolving
	// this makes this layer unnecessarily differ from a cached layer which does contain this information
	hdr.Uname = ""
	hdr.Gname = ""
	// use PAX format to preserve accurate mtime (match Docker behavior)
	hdr.Format = tar.FormatPAX

	hardlink, linkDst := t.checkHardlink(p, i)
	if hardlink {
		hdr.Linkname = linkDst
		hdr.Typeflag = tar.TypeLink
		hdr.Size = 0
	}

	name := entryName(hdr.Name)
	// An entry alongside its own whiteout contradicts itself; extraction
	// behavior would be undefined.
	whiteout := entryName(filepath.Join(filepath.Dir(name), archive.WhiteoutPrefix+filepath.Base(name)))
	_, conflicting := t.seen[whiteout]
	assert.Assert("tar.whiteout-conflict", !conflicting, "tar entry %q must not coexist with its whiteout", name)
	if hardlink {
		// A hardlink can only be extracted if its target was already written
		// to the same tar.
		_, targetSeen := t.seen[entryName(linkDst)]
		assert.Assert("tar.hardlink-target-in-tar", targetSeen, "hardlink %q target %q must already be in the tar", name, entryName(linkDst))
	}
	t.assertEntry(name)

	if err := t.w.WriteHeader(hdr); err != nil {
		return err
	}
	if !(i.Mode().IsRegular()) || hardlink {
		return nil
	}
	r, err := FSys.Open(p)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := io.Copy(t.w, r); err != nil {
		return err
	}
	return nil
}

// writeSecurityXattrToTarFile writes security.capability
// xattrs from a tar header to filesystem
func writeSecurityXattrToTarFile(path string, hdr *tar.Header) error {
	if hdr.PAXRecords == nil {
		return nil
	}
	if capability, ok := hdr.PAXRecords["SCHILY.xattr."+securityCapabilityXattr]; ok {
		err := Lsetxattr(path, securityCapabilityXattr, []byte(capability), 0)
		if err != nil && !errors.Is(err, syscall.EOPNOTSUPP) && !errors.Is(err, ErrNotSupportedPlatform) {
			return fmt.Errorf("failed to write %q attribute to %q: %w", securityCapabilityXattr, path, err)
		}
	}
	return nil
}

// readSecurityXattrToTarHeader reads security.capability
// xattrs from filesystem to a tar header
func readSecurityXattrToTarHeader(path string, hdr *tar.Header) error {
	if hdr.PAXRecords == nil {
		hdr.PAXRecords = make(map[string]string)
	}
	capability, err := Lgetxattr(path, securityCapabilityXattr)
	if err != nil {
		return fmt.Errorf("failed to read %q attribute from %q: %w", securityCapabilityXattr, path, err)
	}
	if capability != nil {
		hdr.PAXRecords["SCHILY.xattr."+securityCapabilityXattr] = string(capability)
	}
	return nil
}

func (t *Tar) Whiteout(p string) error {
	dir := filepath.Dir(p)
	name := archive.WhiteoutPrefix + filepath.Base(p)

	th := &tar.Header{
		// Docker uses no leading / in the tarball
		Name: strings.TrimLeft(filepath.Join(dir, name), "/"),
		Size: 0,
	}

	// A whiteout alongside the entry it deletes contradicts itself; extraction
	// behavior would be undefined.
	_, conflicting := t.seen[entryName(p)]
	assert.Assert("tar.whiteout-conflict", !conflicting, "whiteout %q must not coexist with the entry it deletes", th.Name)
	t.assertEntry(entryName(th.Name))

	if err := t.w.WriteHeader(th); err != nil {
		return err
	}

	return nil
}

// Returns true if path is hardlink, and the link destination
func (t *Tar) checkHardlink(p string, i os.FileInfo) (bool, string) {
	hardlink := false
	linkDst := ""
	stat := getSyscallStatT(i)
	if stat != nil {
		nlinks := stat.Nlink
		if nlinks > 1 {
			inode := stat.Ino
			if original, exists := t.hardlinks[inode]; exists && original != p {
				hardlink = true
				logrus.Debugf("%s inode exists in hardlinks map, linking to %s", p, original)
				linkDst = original
			} else {
				t.hardlinks[inode] = p
			}
		}
	}
	return hardlink, linkDst
}

func getSyscallStatT(i os.FileInfo) *syscall.Stat_t {
	if sys := i.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			return stat
		}
	}
	return nil
}

// UnpackLocalTarArchive unpacks the tar archive at path to the directory dest
// Returns the files extracted from the tar archive
func UnpackLocalTarArchive(path, dest string) ([]string, error) {
	// First, we need to check if the path is a local tar archive
	if compressed, compressionLevel := fileIsCompressedTar(path); compressed {
		file, err := FSys.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		switch compressionLevel {
		case compression.Gzip:
			gzr, err := gzip.NewReader(file)
			if err != nil {
				return nil, err
			}
			defer gzr.Close()
			return UnTar(gzr, dest)
		case compression.Bzip2:
			bzr := bzip2.NewReader(file)
			return UnTar(bzr, dest)
		default:
			logrus.Fatalf("unsupported compression algorithm: %d", compressionLevel)
		}
	}
	if fileIsUncompressedTar(path) {
		file, err := FSys.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return UnTar(file, dest)
	}
	return nil, errors.New("path does not lead to local tar archive")
}

// IsFileLocalTarArchive returns true if the file is a local tar archive
func IsFileLocalTarArchive(src string) bool {
	compressed, _ := fileIsCompressedTar(src)
	uncompressed := fileIsUncompressedTar(src)
	return compressed || uncompressed
}

func fileIsCompressedTar(src string) (bool, compression.Compression) {
	r, err := FSys.Open(src)
	if err != nil {
		return false, -1
	}
	defer r.Close()
	buf, err := io.ReadAll(r)
	if err != nil {
		return false, -1
	}
	compressionLevel := compression.Detect(buf)
	return (compressionLevel > 0), compressionLevel
}

func fileIsUncompressedTar(src string) bool {
	r, err := FSys.Open(src)
	if err != nil {
		return false
	}
	defer r.Close()
	fi, err := os.Lstat(src)
	if err != nil {
		return false
	}
	if fi.Size() == 0 {
		return false
	}
	tr := tar.NewReader(r)
	if tr == nil {
		return false
	}
	_, err = tr.Next()
	return err == nil
}

// UnpackCompressedTar unpacks the compressed tar at path to dir
func UnpackCompressedTar(path, dir string) error {
	file, err := FSys.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()
	_, err = UnTar(gzr, dir)
	return err
}
