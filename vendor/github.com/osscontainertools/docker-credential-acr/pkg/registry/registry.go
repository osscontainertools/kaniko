/*
Copyright © 2020 Chris Mellard chris.mellard@icloud.com

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
package registry

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/containers/azcontainerregistry"
	"github.com/Azure/azure-sdk-for-go/services/preview/containerregistry/runtime/2019-08-15-preview/containerregistry"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
)

const defaultTimeOut = 30 * time.Second

// GetRefreshToken exchanges an Azure AD access token for an OAuth2 refresh token for
// the registry specified by serverURL
func GetRefreshToken(ctx context.Context, serverURL, tenantID, accessToken string) (string, error) {
	client, err := azcontainerregistry.NewAuthenticationClient("https://"+serverURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create authentication client - %w", err)
	}

	rt, err := client.ExchangeAADAccessTokenForACRRefreshToken(ctx,
		azcontainerregistry.PostContentSchemaGrantTypeAccessToken,
		serverURL,
		&azcontainerregistry.AuthenticationClientExchangeAADAccessTokenForACRRefreshTokenOptions{
			AccessToken: &accessToken,
			Tenant:      &tenantID,
		})
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token for container registry - %w", err)
	}
	if rt.RefreshToken == nil {
		return "", fmt.Errorf("no refresh token for container registry %s", serverURL)
	}

	return *rt.RefreshToken, nil
}

// GetRegistryRefreshTokenFromAADExchange retrieves an OAuth2 refresh token for the registry specified by serverURL
//
// The authorizer is the service principal token itself, so the exchange request
// refreshes it again on the way out
func GetRegistryRefreshTokenFromAADExchange(serverURL string, principalToken *adal.ServicePrincipalToken, tenantID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeOut)
	defer cancel()

	// If refreshing fails, don't try again, just fail.
	principalToken.MaxMSIRefreshAttempts = 1

	if err := principalToken.EnsureFreshWithContext(ctx); err != nil {
		return "", fmt.Errorf("error refreshing sp token - %w", err)
	}

	registryName, err := getRegistryURL(serverURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse server URL - %w", err)
	}

	refreshTokenClient := containerregistry.NewRefreshTokensClient(registryName.String())
	refreshTokenClient.Authorizer = autorest.NewBearerAuthorizer(principalToken)

	rt, err := refreshTokenClient.GetFromExchange(ctx, "access_token", serverURL, tenantID, "", principalToken.Token().AccessToken)
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token for container registry - %w", err)
	}
	if rt.RefreshToken == nil {
		return "", fmt.Errorf("no refresh token for container registry %s", serverURL)
	}

	return *rt.RefreshToken, nil
}

// parseRegistryName parses a serverURL and returns the registry name (i.e. minus transport scheme)
func getRegistryURL(serverURL string) (*url.URL, error) {
	sURL, err := url.Parse("https://" + serverURL)
	if err != nil {
		return &url.URL{}, fmt.Errorf("failed to parse server URL - %w", err)
	}

	return sURL, nil
}
