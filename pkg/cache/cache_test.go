package cache

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/osscontainertools/kaniko/pkg/config"
)

func TestLocalSourceForPlatformFollowsIndexDigestAlias(t *testing.T) {
	oldOCIWarmer := config.FF.OCIWarmer
	config.FF.OCIWarmer = true
	t.Cleanup(func() {
		config.FF.OCIWarmer = oldOCIWarmer
	})

	cacheDir := t.TempDir()
	imageDigest, err := empty.Image.Digest()
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(cacheDir, imageDigest.String())
	imageLayout, err := layout.Write(imagePath, empty.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err := imageLayout.AppendImage(empty.Image); err != nil {
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
