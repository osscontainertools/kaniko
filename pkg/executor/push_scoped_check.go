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

// Everything in this file exists only because remote.CheckPushPermission resolves
// credentials against ref.Context().Registry instead of ref.Context(), which discards
// the repository path FF_KANIKO_PATH_SCOPED_REGISTRY_AUTH depends on.
// Reported upstream as google/go-containerregistry#2410, fix proposed in
// google/go-containerregistry#2411.
// Once that lands and the dependency is bumped, delete this file and drop the
// checkPushPermissionScoped branch in CheckPushPermissions.

package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// checkPushPermissionScoped is a repository-aware equivalent of remote.CheckPushPermission,
// used instead of it when FF_KANIKO_PATH_SCOPED_REGISTRY_AUTH is enabled.
// remote.CheckPushPermission hardcodes credential resolution to ref.Context().Registry
// (vendored code, not a kaniko call site we can pass a different resource into),
// which would silently discard the repository path this feature depends on.
//
// This reimplements its blob-upload-initiation probe,
// resolving credentials against ref.Context() (the full repository) instead.
func checkPushPermissionScoped(ref name.Reference, kc authn.Keychain, t http.RoundTripper) error {
	repo := ref.Context()
	auth, err := kc.Resolve(repo)
	if err != nil {
		return fmt.Errorf("resolving authorization for %v failed: %w", repo, err)
	}

	scopes := []string{ref.Scope(transport.PushScope)}
	tr, err := transport.NewWithContext(context.Background(), repo.Registry, auth, t, scopes)
	if err != nil {
		return fmt.Errorf("creating push check transport for %v failed: %w", repo.Registry, err)
	}
	client := &http.Client{Transport: tr}

	u := url.URL{
		Scheme: repo.Registry.Scheme(),
		Host:   repo.RegistryStr(),
		Path:   fmt.Sprintf("/v2/%s/blobs/uploads/", repo.RepositoryStr()),
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := transport.CheckError(resp, http.StatusCreated, http.StatusAccepted); err != nil {
		return err
	}

	if resp.StatusCode == http.StatusAccepted {
		loc, err := scopedUploadLocation(resp)
		if err != nil {
			// A real upload cannot continue without a usable Location.
			// Preserve remote.CheckPushPermission's strict 202 response handling.
			return err
		}
		if loc != "" {
			go cancelScopedUpload(client, loc)
		}
	}

	return nil
}

// scopedUploadLocation resolves the Location header from an upload-initiation response,
// rejecting a redirect to a different host that resolves to a private or link-local IP literal.
// Mirrors the SSRF guard vendor/.../pkg/v1/remote's own nextLocation applies to the regular push path;
// that helper is unexported so it can't be reused directly.
func scopedUploadLocation(resp *http.Response) (string, error) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("missing Location header")
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	resolved := resp.Request.URL.ResolveReference(u)

	origHost := resp.Request.URL.Hostname()
	if destHost := resolved.Hostname(); destHost != origHost {
		if ip := net.ParseIP(destHost); ip != nil {
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
				return "", fmt.Errorf("SSRF protection: Location header redirects to private/link-local host %q", destHost)
			}
		}
	}

	return resolved.String(), nil
}

// cancelScopedUpload best-effort cancels an initiated upload;
// it only exists to prove push permission, so a failed cancel is not an error worth surfacing.
func cancelScopedUpload(client *http.Client, loc string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, loc, nil)
	if err != nil {
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}
