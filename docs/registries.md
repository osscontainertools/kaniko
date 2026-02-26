# Pushing to Different Registries

kaniko uses Docker credential helpers to push images to a registry.

kaniko comes with support for GCR, Docker `config.json` and Amazon ECR, but
configuring another credential helper should allow pushing to a different
registry.

## Credential Provider Priorities

By default kaniko will configure all built-in credential providers for you. These are `[default, env, google, ecr, acr, gitlab]`.
You can (de)-activate credential helpers via the [`--credential-helpers`](#flag---credential-helpers) flag. The `default` credential helper will always be active and itself handles two sources: `DOCKER_AUTH_CONFIG` environment variable and `/kaniko/.docker/config.json` file, where priority is always given to `DOCKER_AUTH_CONFIG` and therefore can shadow credentials configured in the config file. If you want to disable `DOCKER_AUTH_CONFIG` you have to unset the environment variable explicitly `unset DOCKER_AUTH_CONFIG` prior to calling kaniko.

## Pushing to Docker Hub

Get your docker registry user and password encoded in base64

    echo -n USER:PASSWORD | base64

Create a `config.json` file with your Docker registry url and the previous
generated base64 string

**Note:** Please use v1 endpoint. See #1209 for more details

```json
{
  "auths": {
    "https://index.docker.io/v1/": {
      "auth": "xxxxxxxxxxxxxxx"
    }
  }
}
```

Run kaniko with the `config.json` inside `/kaniko/.docker/config.json`

```shell
docker run -ti --rm -v `pwd`:/workspace -v `pwd`/config.json:/kaniko/.docker/config.json:ro gcr.io/kaniko-project/executor:latest --dockerfile=Dockerfile --destination=yourimagename
```

## Pushing to Google GCR

To create a credentials to authenticate to Google Cloud Registry, follow these
steps:

1. Create a
   [service account](https://console.cloud.google.com/iam-admin/serviceaccounts)
   or in the Google Cloud Console project you want to push the final image to
   with `Storage Admin` permissions.
2. Download a JSON key for this service account
3. (optional) Rename the key to `kaniko-secret.json`, if you don't rename, you
   have to change the name used the command(in the volume part)
4. Run the container adding the path in GOOGLE_APPLICATION_CREDENTIALS env var

```shell
docker run -ti --rm -e GOOGLE_APPLICATION_CREDENTIALS=/kaniko/config.json \
-v `pwd`:/workspace -v `pwd`/kaniko-secret.json:/kaniko/config.json:ro gcr.io/kaniko-project/executor:latest \
--dockerfile=Dockerfile --destination=yourimagename
```

## Pushing to GCR using Workload Identity

If you have enabled Workload Identity on your GKE cluster then you can use the
workload identity to push built images to GCR without adding a
`GOOGLE_APPLICATION_CREDENTIALS` in your kaniko pod specification.

Learn more on how to
[enable](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#enable_on_cluster)
and
[migrate existing apps](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#migrate_applications_to)
to workload identity.

To authenticate using workload identity you need to run the kaniko pod using the
Kubernetes Service Account (KSA) bound to Google Service Account (GSA) which has
`Storage.Admin` permissions to push images to Google Container registry.

Please follow the detailed steps
[here](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#authenticating_to)
to create a Kubernetes Service Account, Google Service Account and create an IAM
policy binding between the two to allow the Kubernetes Service account to act as
the Google service account.

To grant the Google Service account the right permission to push to GCR, run the
following GCR command

```
gcloud projects add-iam-policy-binding $PROJECT \
  --member=serviceAccount:[gsa-name]@${PROJECT}.iam.gserviceaccount.com \
  --role=roles/storage.objectAdmin
```

Please ensure, kaniko pod is running in the namespace and with a Kubernetes
Service Account.

## Pushing to Amazon ECR

The Amazon ECR
[credential helper](https://github.com/awslabs/amazon-ecr-credential-helper) is
built into the kaniko executor image.

1. Configure credentials

   1. You can use instance roles when pushing to ECR from a EC2 instance or from
      EKS, by
      [configuring the instance role permissions](https://docs.aws.amazon.com/AmazonECR/latest/userguide/ECR_on_EKS.html)
      (the AWS managed policy
      `EC2InstanceProfileForImageBuilderECRContainerBuilds` provides broad
      permissions to upload ECR images and may be used as configuration
      baseline). Additionally, set `AWS_SDK_LOAD_CONFIG=true` as environment
      variable within the kaniko pod. If running on an EC2 instance with an
      instance profile, you may also need to set
      `AWS_EC2_METADATA_DISABLED=true` for kaniko to pick up the correct
      credentials.

   2. Or you can create a Kubernetes secret for your `~/.aws/credentials` file
      so that credentials can be accessed within the cluster. To create the
      secret, run:
      `shell kubectl create secret generic aws-secret --from-file=<path to .aws/credentials> `

The Kubernetes Pod spec should look similar to this, with the args parameters
filled in. Note that `aws-secret` volume mount and volume are only needed when
using AWS credentials from a secret, not when using instance roles.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kaniko
spec:
  containers:
    - name: kaniko
      image: gcr.io/kaniko-project/executor:latest
      args:
        - "--dockerfile=<path to Dockerfile within the build context>"
        - "--context=s3://<bucket name>/<path to .tar.gz>"
        - "--destination=<aws_account_id.dkr.ecr.region.amazonaws.com/my-repository:my-tag>"
      volumeMounts:
        # when not using instance role
        - name: aws-secret
          mountPath: /root/.aws/
  restartPolicy: Never
  volumes:
    # when not using instance role
    - name: aws-secret
      secret:
        secretName: aws-secret
```

## Pushing to Azure Container Registry

An ACR
[credential helper](https://github.com/chrismellard/docker-credential-acr-env)
is built into the kaniko executor image, which can be used to authenticate with
well-known Azure environmental information.

To configure credentials, you will need to do the following:

1. Update the `credStore` section of `config.json`:

```json
{ "credsStore": "acr" }
```

A downside of this approach is that ACR authentication will be used for all
registries, which will fail if you also pull from DockerHub, GCR, etc. Thus, it
is better to configure the credential tool only for your ACR registries by using
`credHelpers` instead of `credsStore`:

```json
{ "credHelpers": { "mycr.azurecr.io": "acr-env" } }
```

You can mount in the new config as a configMap:

```shell
kubectl create configmap docker-config --from-file=<path to config.json>
```

2. Configure credentials

You can create a Kubernetes secret with environment variables required for
Service Principal authentication and expose them to the builder container.

```
AZURE_CLIENT_ID=<clientID>
AZURE_CLIENT_SECRET=<clientSecret>
AZURE_TENANT_ID=<tenantId>
```

If the above are not set then authentication falls back to managed service
identities and the MSI endpoint is attempted to be contacted which will work in
various Azure contexts such as App Service and Azure Kubernetes Service where
the MSI endpoint will authenticate the MSI context the service is running under.

`AZURE_ENVIRONMENT=<environment>` can be used to connect to clouds other than the standard public cloud.
Choose among:
  * `AZUREPUBLICCLOUD` (default)
  * `AZURECHINACLOUD`
  * `AZUREGERMANCLOUD`
  * `AZUREUSGOVERNMENT`

The Kubernetes Pod spec should look similar to this, with the args parameters
filled in. Note that `azure-secret` secret is only needed when using Azure
Service Principal credentials, not when using a managed service identity.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kaniko
spec:
  containers:
    - name: kaniko
      image: gcr.io/kaniko-project/executor:latest
      args:
        - "--dockerfile=<path to Dockerfile within the build context>"
        - "--context=s3://<bucket name>/<path to .tar.gz>"
        - "--destination=mycr.azurecr.io/my-repository:my-tag"
      envFrom:
        # when authenticating with service principal
        - secretRef:
            name: azure-secret
      volumeMounts:
        - name: docker-config
          mountPath: /kaniko/.docker/
  volumes:
    - name: docker-config
      configMap:
        name: docker-config
  restartPolicy: Never
```

## Pushing to JFrog Container Registry or to JFrog Artifactory

Kaniko can be used with both
[JFrog Container Registry](https://www.jfrog.com/confluence/display/JFROG/JFrog+Container+Registry)
and JFrog Artifactory.

Get your JFrog Artifactory registry user and password encoded in base64

    echo -n USER:PASSWORD | base64

Create a `config.json` file with your Artifactory Docker local registry URL and
the previous generated base64 string

```json
{
  "auths": {
    "artprod.company.com": {
      "auth": "xxxxxxxxxxxxxxx"
    }
  }
}
```

For example, for Artifactory cloud users, the docker registry should be:
`<company>.<local-repository-name>.io`.

Run kaniko with the `config.json` inside `/kaniko/.docker/config.json`

    docker run -ti --rm -v `pwd`:/workspace -v `pwd`/config.json:/kaniko/.docker/config.json:ro gcr.io/kaniko-project/executor:latest --dockerfile=Dockerfile --destination=yourimagename

After the image is uploaded, using the JFrog CLI, you can
[collect](https://www.jfrog.com/confluence/display/CLI/CLI+for+JFrog+Artifactory#CLIforJFrogArtifactory-PushingDockerImagesUsingKaniko)
and
[publish](https://www.jfrog.com/confluence/display/CLI/CLI+for+JFrog+Artifactory#CLIforJFrogArtifactory-PublishingBuild-Info)
the build information to Artifactory and trigger
[build vulnerabilities scanning](https://www.jfrog.com/confluence/display/JFROG/Declarative+Pipeline+Syntax#DeclarativePipelineSyntax-ScanningBuildswithJFrogXray)
using JFrog Xray.

To collect and publish the image's build information using the Jenkins
Artifactory plugin, see instructions for
[scripted pipeline](https://www.jfrog.com/confluence/display/JFROG/Scripted+Pipeline+Syntax#ScriptedPipelineSyntax-UsingKaniko)
and
[declarative pipeline](https://www.jfrog.com/confluence/display/JFROG/Declarative+Pipeline+Syntax#DeclarativePipelineSyntax-UsingKaniko).
