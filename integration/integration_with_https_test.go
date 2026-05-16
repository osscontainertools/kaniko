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

// TestHTTPSBuildcontext exercises the https:// tarball build context. The
// scripts/setup-tls-registry-creds.sh helper (invoked by k3s-setup) generates a
// self-signed cert for IP:127.0.0.2, which we reuse to serve the tarball over
// TLS on a free port. Docker consumes the same tarball over stdin so it does
// not have to trust the self-signed cert; kaniko trusts it via a bind mount
// into its SSL_CERT_DIR.
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

	tarPath := filepath.Join(t.TempDir(), "context.tar.gz")
	tarCmd := exec.Command("tar", "-czf", tarPath, "Dockerfile_test_run_2")
	tarCmd.Dir = dockerfilesPath
	tarOut, err := tarCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tar: %v\n%s", err, tarOut)
	}

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

	// Build with docker by piping the same tarball over stdin so the daemon
	// does not have to trust the self-signed cert.
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_test_https")
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

	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_test_https")
	dockerRunFlags := []string{
		"run", "--net=host",
		"-v", caCert + ":/kaniko/ssl/certs/test-server-ca.crt:ro",
	}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-c", tarballURL,
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
