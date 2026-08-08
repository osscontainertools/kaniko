/*
Copyright 2020 Google LLC

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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/connstats"
	"github.com/sirupsen/logrus"
)

type CertPool interface {
	value() *x509.CertPool
	append(path string) error
}

type X509CertPool struct {
	inner x509.CertPool
}

func (p *X509CertPool) value() *x509.CertPool {
	return &p.inner
}

func (p *X509CertPool) append(path string) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p.inner.AppendCertsFromPEM(pem)
	return nil
}

var systemCertLoader CertPool

type KeyPairLoader interface {
	load(string, string) (tls.Certificate, error)
}

type X509KeyPairLoader struct{}

func (p *X509KeyPairLoader) load(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}

var systemKeyPairLoader KeyPairLoader

func init() {
	systemCertPool, err := x509.SystemCertPool()
	if err != nil {
		logrus.Warn("Failed to load system cert pool. Loading empty one instead.")
		systemCertPool = x509.NewCertPool()
	}
	systemCertLoader = &X509CertPool{
		inner: *systemCertPool,
	}

	systemKeyPairLoader = &X509KeyPairLoader{}
}

// connstats.Trace drops CloseIdleConnections, which nothing notices while
// go-containerregistry's own wrapper drops it too.
func init() {
	_, forwards := any(transport.NewRetry(nil)).(interface{ CloseIdleConnections() })
	assert.Assert("util.transport.close-idle-dropped", !forwards, "go-containerregistry forwards CloseIdleConnections, so the connstats wrapper has to forward it as well")
}

// MakeTransport returns a transport for registryName, wired up to count the
// sockets and requests it makes.
func MakeTransport(opts config.RegistryOptions, registryName string) (http.RoundTripper, error) {
	tr, err := makeTransport(opts, registryName)
	if err != nil {
		return nil, err
	}
	tr.DialContext = connstats.WrapDial(tr.DialContext)
	return connstats.Trace(tr), nil
}

func makeTransport(opts config.RegistryOptions, registryName string) (*http.Transport, error) {
	// Create a transport to set our user-agent.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if opts.SkipTLSVerify || opts.SkipTLSVerifyRegistries.Contains(registryName) {
		tr.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if certificatePath := opts.RegistriesCertificates[registryName]; certificatePath != "" {
		if err := systemCertLoader.append(certificatePath); err != nil {
			return nil, fmt.Errorf("failed to load certificate %s for %s: %w", certificatePath, registryName, err)
		}
		tr.TLSClientConfig = &tls.Config{
			RootCAs: systemCertLoader.value(),
		}
	}

	if clientCertificatePath := opts.RegistriesClientCertificates[registryName]; clientCertificatePath != "" {
		certFiles := strings.Split(clientCertificatePath, ",")
		if len(certFiles) != 2 {
			return nil, fmt.Errorf("failed to load client certificate/key '%s=%s', expected format: %s=/path/to/cert,/path/to/key", registryName, clientCertificatePath, registryName)
		}
		cert, err := systemKeyPairLoader.load(certFiles[0], certFiles[1])
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate/key '%s' for %s: %w", clientCertificatePath, registryName, err)
		}
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}

	if config.FF.DisableHTTP2 {
		tr.ForceAttemptHTTP2 = false
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
	}

	return tr, nil
}

// LogRegistryConnections reports how the build used its registry sockets.
func LogRegistryConnections() {
	stats := connstats.Snapshot()
	logrus.Debugf("registry connections: sockets opened=%d closed=%d open=%d peak=%d, requests=%d reused=%d, tls handshakes=%d in %v, dialing %v, idle before reuse %v",
		stats.SocketsOpened, stats.SocketsClosed, stats.SocketsOpen, stats.PeakSockets,
		stats.Requests, stats.Reused, stats.TLSHandshakes, stats.TLSTime.Round(time.Millisecond),
		stats.DialTime.Round(time.Millisecond), stats.IdleTime.Round(time.Millisecond))
}
