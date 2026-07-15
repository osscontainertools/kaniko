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

package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var fixtureModTime = time.Unix(1600000000, 0)

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	content  string
}

func buildTar(entries []tarEntry) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			ModTime:  fixtureModTime,
		}
		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		err := tw.WriteHeader(hdr)
		if err != nil {
			return nil, err
		}
		if e.typeflag == tar.TypeReg {
			_, err = tw.Write([]byte(e.content))
			if err != nil {
				return nil, err
			}
		}
	}
	err := tw.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipBytes(in []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(in)
	if err != nil {
		return nil, err
	}
	err = zw.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bzip2Bytes shells out to bzip2 because the standard library has no bzip2 writer.
func bzip2Bytes(in []byte) ([]byte, error) {
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(in)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("bzip2: %w", err)
	}
	return out.Bytes(), nil
}

var tarFixtureNames = []string{"file.tar", "file.tar.gz", "file.bz2", "sys.tar.gz"}

func tarFixtureDir() string {
	_, ex, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(ex), "context", "tars")
}

// generateTarFixtures writes the tar archives consumed by the ADD integration
// tests into context/tars, so the repository holds no committed binary archives.
func generateTarFixtures() error {
	dir := tarFixtureDir()
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return err
	}

	plain, err := buildTar([]tarEntry{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./uncompressedFile", typeflag: tar.TypeReg, mode: 0o644},
	})
	if err != nil {
		return err
	}

	gzipped, err := buildTar([]tarEntry{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./compressedFile", typeflag: tar.TypeReg, mode: 0o644, content: "compressed\n"},
	})
	if err != nil {
		return err
	}

	bzipped, err := buildTar([]tarEntry{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./bzCompressedFile", typeflag: tar.TypeReg, mode: 0o644, content: "bzip\n"},
	})
	if err != nil {
		return err
	}

	sys, err := buildTar([]tarEntry{
		{name: "sys/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "sys/fs/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "sys/fs/foo", typeflag: tar.TypeReg, mode: 0o644},
	})
	if err != nil {
		return err
	}

	gzippedGz, err := gzipBytes(gzipped)
	if err != nil {
		return err
	}
	sysGz, err := gzipBytes(sys)
	if err != nil {
		return err
	}
	bzippedBz, err := bzip2Bytes(bzipped)
	if err != nil {
		return err
	}

	fixtures := map[string][]byte{
		"file.tar":    plain,
		"file.tar.gz": gzippedGz,
		"file.bz2":    bzippedBz,
		"sys.tar.gz":  sysGz,
	}
	for name, data := range fixtures {
		err = os.WriteFile(filepath.Join(dir, name), data, 0o644)
		if err != nil {
			return err
		}
	}
	return nil
}

func removeTarFixtures() {
	dir := tarFixtureDir()
	for _, name := range tarFixtureNames {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
