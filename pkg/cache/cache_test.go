package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/osscontainertools/kaniko/pkg/config"
)

func TestLocalSourceForPlatformFollowsIndexDigestAlias(t *testing.T) {
	cacheDir := t.TempDir()
	imageDigest, err := empty.Image.Digest()
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(cacheDir, imageDigest.String())
	imageFile, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag("example.com/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := tarball.Write(ref, empty.Image, imageFile); err != nil {
		_ = imageFile.Close()
		t.Fatal(err)
	}
	if err := imageFile.Close(); err != nil {
		t.Fatal(err)
	}

	indexDigest := "sha256:" + strings.Repeat("a", 64)
	if _, err := v1.NewHash(indexDigest); err != nil {
		t.Fatal(err)
	}
	options := &config.CacheOptions{CacheDir: cacheDir, CacheTTL: time.Hour}
	if err := StoreDigestAlias(options, indexDigest, imageDigest.String(), "linux/arm64"); err != nil {
		t.Fatal(err)
	}

	cachedImage, err := LocalSourceForPlatform(options, indexDigest, "linux/arm64")
	if err != nil {
		t.Fatalf("expected index alias to resolve: %v", err)
	}
	cachedDigest, err := cachedImage.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if cachedDigest != imageDigest {
		t.Fatalf("expected platform image digest %s, got %s", imageDigest, cachedDigest)
	}

	if _, err := LocalSourceForPlatform(options, indexDigest, "linux/amd64"); !IsNotFound(err) {
		t.Fatalf("expected another platform to miss, got %v", err)
	}
}
