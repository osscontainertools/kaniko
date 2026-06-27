# Bug: OCI layer descriptor annotations missing from kaniko output

## Status

Hotfixed via `--extra-ignore-annotations` in `TestBuildWithAnnotations` and pinning `FROM ubuntu` to Noble in affected test Dockerfiles (#680). Proper fix is open.

## Summary

When building from an OCI base image, Docker BuildKit adds `ci.umo.uncompressed_blob_size` annotations to every layer descriptor in the output OCI manifest. Kaniko strips base-image layer annotations via `withoutAnnotations()` and does not add them to layers it creates. The outputs therefore diverge structurally even when the image contents are identical.

A second related gap: the `--annotation` flag in kaniko does not reliably propagate to OCI manifest annotations when the base image is OCI. Docker BuildKit does propagate them. This was previously masked because the test (`TestBuildWithAnnotations`) compared against Docker's full manifest diff; when both sides had no manifest annotations the diff was empty.

## Background

### What triggered it

`ubuntu:latest` moved to Ubuntu 26.04 "Resolute" around 2026-05-04. Resolute's OCI manifest carries `ci.umo.uncompressed_blob_size` annotations on its layer descriptors. Docker BuildKit propagates these to the output manifest and also adds them to any new layers it creates (e.g. from `RUN` instructions). Kaniko strips them. CI tests comparing Docker vs kaniko output with `diffoci` then failed.

Noble (ubuntu:24.04) has no layer annotations, so tests that pin to Noble are unaffected by the annotation divergence — but `TestBuildWithAnnotations` fetches its Dockerfile from the `main` branch at test time (git-URL build context always resolves to `#main`), so a Dockerfile pin in a PR branch can't take effect until merged.

### Affected code

`pkg/executor/build.go` — `withoutAnnotations` is called at two points (~line 965 and ~line 1051) to strip annotations from the base image before building. This is correct for preventing stale base-image annotations from leaking into output, but kaniko then never re-adds the annotations that BuildKit would produce for the final layers.

`pkg/executor/build.go:1138` — `mutate.Annotations(sourceImage, opts.Annotations)` applies user-supplied `--annotation` flags. The go-containerregistry `mutate.Annotations` call sets the OCI manifest-level `annotations` field. However, subsequent mutations (e.g. `mutate.AppendLayers`) return a new `v1.Image` that does not carry forward manifest-level annotations, so the annotation may silently disappear depending on call order.

## Proper fix

### Layer descriptor annotations

When kaniko creates or re-emits an OCI manifest, compute `ci.umo.uncompressed_blob_size` for each layer (the uncompressed size is available from `layer.Uncompressed()`) and set it as an annotation on the layer descriptor. This should only apply when the output format is OCI (`application/vnd.oci.image.manifest.v1+json`).

Alternatively, verify whether `diffoci --extra-ignore-annotations` is the right long-term stance (i.e. treat layer descriptor annotations as noise in comparisons). That depends on whether any tooling relies on `ci.umo.uncompressed_blob_size` for correctness.

### `--annotation` propagation

Audit the call order in `pkg/executor/build.go`. The `mutate.Annotations` call must happen **after** all `mutate.AppendLayers` / `mutate.Config` calls, because those calls return a new `image` object that drops manifest-level annotations. The fix is to move the annotation application to the very end of the image construction pipeline, after all layer and config mutations are complete.

### Test

Restore the `getImageManifestAnnotations` check that was removed in commit `7675a2db3`. The test should:

1. Build with `--annotation myannotation=myvalue` via both Docker and kaniko.
2. Fetch the manifest for the kaniko-built image from the registry.
3. Assert `manifest.Annotations["myannotation"] == "myvalue"`.

This is independent of Docker's output format and will catch regressions without being sensitive to BuildKit-specific OCI extensions.
