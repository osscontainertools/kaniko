# Provenance

`--provenance` attaches a [SLSA v1 provenance](https://slsa.dev/spec/v1.0/provenance) attestation to the pushed image. Inspect it with:

```bash
docker buildx imagetools inspect registry.example.com/app:v1 --format '{{json .Provenance}}'
```

The attestation is an [in-toto v1 statement](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md). Its subject is the image manifest, named once per destination. This page describes the predicate, whose `buildType` is `https://github.com/osscontainertools/kaniko/blob/main/docs/provenance.md`.

## What is recorded

| field | content |
| --- | --- |
| `buildDefinition.externalParameters.context` | the repository URL of the build context |
| `buildDefinition.externalParameters.dockerfile` | the Dockerfile path, relative to the context |
| `buildDefinition.externalParameters.target` | the `--target` stages |
| `buildDefinition.externalParameters.platform` | the target platform |
| `buildDefinition.internalParameters.kanikoVersion` | the kaniko version |
| `buildDefinition.internalParameters.featureFlags` | the `FF_KANIKO_*` variables that are set, with their values |
| `buildDefinition.resolvedDependencies` | the base images and `COPY --from` images as `pkg:docker` purls with their digest, and the commit of the context as `git+<url>` with a `gitCommit` digest |
| `runDetails.builder.id` | the CI job URL, the workflow run URL on GitHub Actions, otherwise `https://github.com/osscontainertools/kaniko` |
| `runDetails.metadata.invocationId` | the CI pipeline, job and attempt ids |
| `runDetails.metadata.startedOn`, `finishedOn` | the build start and the time the image was created |

With `--reproducible` the `metadata` is left out.

Never recorded: build arg values, `--secret` values, registry credentials and the contents of the build context.
