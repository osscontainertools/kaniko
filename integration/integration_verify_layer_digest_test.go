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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ggcrtypes "github.com/google/go-containerregistry/pkg/v1/types"
)

// TestVerifyLayerDigest pulls the base through a proxy that answers one layer digest with
// other bytes, the way a compromised mirror or a corrupting cache would. kaniko must refuse
// the layer instead of building on it.
func TestVerifyLayerDigest(t *testing.T) {
	genuineTar, err := buildTar([]tarEntry{
		{"probe/a", tar.TypeReg, 0o644, "genuine\n"},
		{"probe/b", tar.TypeReg, 0o644, "genuine\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tamperedTar, err := buildTar([]tarEntry{{"probe/a", tar.TypeReg, 0o644, "tampered\n"}})
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := gzipBytes(tamperedTar)
	if err != nil {
		t.Fatal(err)
	}

	src, err := name.ParseReference(config.busyboxBaseImage, name.WeakValidation)
	if err != nil {
		t.Fatal(err)
	}
	base, err := remote.Image(src, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		t.Fatal(err)
	}
	man, err := base.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	mediaType := ggcrtypes.DockerLayer
	if strings.Contains(string(man.MediaType), ggcrtypes.OCIVendorPrefix) {
		mediaType = ggcrtypes.OCILayer
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(genuineTar)), nil
	}, tarball.WithMediaType(mediaType), tarball.WithCompressionLevel(gzip.NoCompression))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		t.Fatal(err)
	}
	genuineBlob, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	// stored deflate blocks carry the file bytes verbatim, so the edit still inflates and only the gzip CRC and the digest disagree
	corrupted := bytes.Replace(genuineBlob, []byte("genuine\n"), []byte("corrupt\n"), 1)
	if bytes.Equal(corrupted, genuineBlob) {
		t.Fatal("the genuine layer blob does not hold the file bytes verbatim")
	}
	img, err := mutate.AppendLayers(base, layer)
	if err != nil {
		t.Fatal(err)
	}
	repo := config.imageRepo + "test_verify_layer_digest"
	baseRef, err := name.ParseReference(repo+":base", name.WeakValidation)
	if err != nil {
		t.Fatal(err)
	}
	err = remote.Write(baseRef, img, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		t.Fatal(err)
	}
	imgDigest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	layerDigest, err := layer.Digest()
	if err != nil {
		t.Fatal(err)
	}
	layerSize, err := layer.Size()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		blob []byte
	}{{
		name: "different content",
		blob: tampered,
	}, {
		name: "corrupted gzip",
		blob: corrupted,
	}}

	upstream := &url.URL{Scheme: "http", Host: baseRef.Context().RegistryStr()}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// ggcr cuts the body at the manifest size, so a longer blob would fail as a broken gzip stream
			if int64(len(tc.blob)) > layerSize {
				t.Fatalf("replacement blob has %d bytes, it must not exceed the %d byte layer", len(tc.blob), layerSize)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/blobs/"+layerDigest.String()) {
					w.Header().Set("Content-Type", "application/octet-stream")
					w.WriteHeader(http.StatusOK)
					// flushing before the body drops Content-Length, which ggcr would compare against the manifest size
					w.(http.Flusher).Flush()
					_, _ = w.Write(tc.blob)
					return
				}
				proxy.ServeHTTP(w, r)
			}))
			defer srv.Close()

			proxied := strings.TrimPrefix(srv.URL, "http://") + "/" + baseRef.Context().RepositoryStr() + "@" + imgDigest.String()
			// the destination repo already holds the genuine base blobs, so the push skips them and ships the base unread
			destination := fmt.Sprintf("%s:%s-%d", repo, strings.ReplaceAll(tc.name, " ", "-"), time.Now().UnixNano())
			out, buildErr := buildKanikoImage(
				t,
				dockerfilesPath,
				"Dockerfile_verify_layer_digest",
				[]string{"--build-arg", "BASE=" + proxied},
				[]string{"-c", buildContextPath},
				destination,
				config.dir,
				"", "",
			)

			dst, err := name.ParseReference(destination, name.WeakValidation)
			if err != nil {
				t.Fatal(err)
			}
			shipped, err := remote.Image(dst, remote.WithAuthFromKeychain(authn.DefaultKeychain))
			switch {
			case err == nil:
				layers, err := shipped.Layers()
				if err != nil {
					t.Fatal(err)
				}
				rc, err := layers[len(layers)-1].Uncompressed()
				if err != nil {
					t.Fatal(err)
				}
				defer rc.Close()
				tr := tar.NewReader(rc)
				seen := ""
				for {
					hdr, err := tr.Next()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if hdr.Name == "seen.txt" {
						raw, err := io.ReadAll(tr)
						if err != nil {
							t.Fatal(err)
						}
						seen = string(raw)
					}
				}
				t.Errorf("kaniko pushed %s on top of base layer %s, but /seen.txt is %q instead of %q", destination, layerDigest, seen, "genuine\na\nb\n")
			case buildErr == nil:
				t.Fatalf("build succeeded but %s is not in the registry: %v", destination, err)
			case !strings.Contains(string(out), "verifying layer"):
				t.Errorf("build failed without a layer verification error: %v", buildErr)
			}
		})
	}
}
