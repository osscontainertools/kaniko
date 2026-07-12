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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeDockerfileTarball builds a gzipped tarball at a fresh temp path
// containing just Dockerfile_test_run_2 at the tarball root.
func makeDockerfileTarball(t *testing.T) string {
	t.Helper()
	tarPath := filepath.Join(t.TempDir(), "context.tar.gz")
	tarCmd := exec.Command("tar", "-czf", tarPath, "Dockerfile_test_run_2")
	tarCmd.Dir = dockerfilesPath
	out, err := tarCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	return tarPath
}

// testBuildFromTarballHelper builds the image with docker (piping tarPath over
// stdin so the daemon doesn't have to handle whatever transport kaniko uses)
// and with kaniko (using kanikoContextRef and any extra docker-run flags such
// as volume mounts), then compares the two images.
func testBuildFromTarballHelper(t *testing.T, imageName, tarPath, kanikoContextRef string, extraDockerRunFlags ...string) {
	t.Helper()

	dockerImage := GetDockerImage(config.imageRepo, imageName)
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
	defer f.Close()
	dockerCmd := exec.Command("docker", "build", "--push",
		"-f", "Dockerfile_test_run_2", "-t", dockerImage, "-")
	dockerCmd.Stdin = f
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Fatalf("docker build failed: %v\n%s", err, out)
	}

	kanikoImage := GetKanikoImage(config.imageRepo, imageName)
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = append(dockerRunFlags, extraDockerRunFlags...)
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-c", kanikoContextRef,
		"-f", "Dockerfile_test_run_2",
		"-d", kanikoImage,
	)
	kanikoCmd := exec.Command("docker", dockerRunFlags...)
	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Fatalf("kaniko build failed: %v\n%s", err, out)
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic",
		"--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

// TestHTTPSBuildcontext exercises the https:// tarball build context. The
// scripts/setup-tls-registry-creds.sh helper (invoked by k3s-setup) generates a
// self-signed cert for IP:127.0.0.2, which we reuse to serve the tarball over
// TLS on a free port. Kaniko trusts the cert via a bind mount into its
// SSL_CERT_DIR.
func TestHTTPSBuildcontext(t *testing.T) {
	t.Parallel()

	caCert := os.Getenv("TLS_REGISTRY_CERT")
	if caCert == "" {
		t.Fatal("TLS_REGISTRY_CERT not set")
	}
	keyFile := filepath.Join(filepath.Dir(caCert), "tls.key")
	cert, err := tls.LoadX509KeyPair(caCert, keyFile)
	if err != nil {
		t.Fatalf("load TLS keypair: %v", err)
	}

	tarPath := makeDockerfileTarball(t)

	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, tarPath)
		}),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() { _ = srv.ServeTLS(listener, "", "") }()
	defer srv.Close()

	tarballURL := fmt.Sprintf("https://%s/context.tar.gz", listener.Addr().String())

	testBuildFromTarballHelper(t, "Dockerfile_test_https", tarPath, tarballURL,
		"-v", caCert+":/kaniko/ssl/certs/test-server-ca.crt:ro")
}

// TestTarFileBuildcontext exercises the tar://<file-path> build context — the
// non-stdin branch of pkg/buildcontext/tar.go. The tarball is created on the
// host and bind-mounted into the kaniko container.
func TestTarFileBuildcontext(t *testing.T) {
	t.Parallel()

	tarPath := makeDockerfileTarball(t)
	const containerTarPath = "/workspace/context.tar.gz"
	testBuildFromTarballHelper(t, "Dockerfile_test_tar_file", tarPath, "tar://"+containerTarPath,
		"-v", tarPath+":"+containerTarPath+":ro")
}
