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
package token

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

func GetAccessToken(ctx context.Context) (accessToken string, tenantID string, err error) {
	config, armAudience, err := cloudConfig(os.Getenv("AZURE_ENVIRONMENT"))
	if err != nil {
		return "", "", err
	}

	scope := tokenResource(armAudience) + "/.default"

	cred, err := chainedCredential(azcore.ClientOptions{Cloud: config})
	if err != nil {
		return "", "", err
	}

	tk, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{scope}})
	if err != nil {
		return "", "", fmt.Errorf("failed to acquire access token - %w", err)
	}

	return tk.Token, os.Getenv("AZURE_TENANT_ID"), nil
}

// chainedCredential walks the same routes as getServicePrincipalToken, in the same
// order. Spelled out rather than DefaultAzureCredential, which would also reach for
// Azure CLI, Azure Developer CLI and PowerShell. ClientAssertionCredential rather
// than WorkloadIdentityCredential, which only knows the file form of the JWT.
func chainedCredential(options azcore.ClientOptions) (azcore.TokenCredential, error) {
	var credentials []azcore.TokenCredential

	environment, err := azidentity.NewEnvironmentCredential(&azidentity.EnvironmentCredentialOptions{ClientOptions: options})
	if err == nil {
		credentials = append(credentials, environment)
	}

	clientID, hasClientID := os.LookupEnv("AZURE_CLIENT_ID")
	tenantID, hasTenantID := os.LookupEnv("AZURE_TENANT_ID")
	_, jwtErr := jwtLookup()
	if hasClientID && hasTenantID && jwtErr == nil {
		assertion, err := azidentity.NewClientAssertionCredential(tenantID, clientID,
			func(context.Context) (string, error) { return jwtLookup() },
			&azidentity.ClientAssertionCredentialOptions{ClientOptions: options})
		if err != nil {
			return nil, fmt.Errorf("failed to initialise assertion credential - %w", err)
		}
		credentials = append(credentials, assertion)
	}

	managedIdentityOptions := azidentity.ManagedIdentityCredentialOptions{ClientOptions: options}
	if clientID != "" {
		managedIdentityOptions.ID = azidentity.ClientID(clientID)
	}
	managedIdentity, err := azidentity.NewManagedIdentityCredential(&managedIdentityOptions)
	if err == nil {
		credentials = append(credentials, managedIdentity)
	}

	if len(credentials) == 0 {
		return nil, fmt.Errorf("no azure credentials found in the environment")
	}

	return azidentity.NewChainedTokenCredential(credentials, nil)
}

// cloudConfig resolves AZURE_ENVIRONMENT against the names go-autorest accepts. The
// ARM audience is spelled out because cloud.Configuration carries an empty Services
// map until azcore/arm/runtime's init fills it in, and we do not import that.
func cloudConfig(name string) (cloud.Configuration, string, error) {
	switch strings.ToUpper(name) {
	case "", "AZURECLOUD", "AZUREPUBLICCLOUD":
		return cloud.AzurePublic, "https://management.core.windows.net/", nil
	case "AZURECHINACLOUD":
		return cloud.AzureChina, "https://management.core.chinacloudapi.cn/", nil
	case "AZUREUSGOVERNMENT", "AZUREUSGOVERNMENTCLOUD":
		return cloud.AzureGovernment, "https://management.core.usgovcloudapi.net/", nil
	default:
		return cloud.Configuration{}, "", fmt.Errorf("AZURE_ENVIRONMENT %q is not supported, azidentity has no configuration for it", name)
	}
}
