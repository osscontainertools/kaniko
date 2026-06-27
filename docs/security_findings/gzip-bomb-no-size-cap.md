# Gzip-bomb / no size cap on layer extraction

**Severity:** Medium — DoS via disk-exhaustion / build-time blowout.
**Affects:** `pkg/util/tar_util.go:UnpackLocalTarArchive` and `pkg/util/fs_util.go:GetFSFromLayers`. Neither applies a maximum-size cap when extracting a compressed tar.
**Reachable via:** any malicious base image's layer or a user-supplied context tar.

## Demonstration

A 51 KB gzipped tar containing one zero-filled file expanded to 52 MB on disk in 60 ms. Compression ratio: **1027×**. Scaling that to a few MB of attacker input fills a host disk in seconds.

```
compressed=51049 bytes  uncompressed=52430336 bytes  ratio=1027x
extracted size = 52428800 bytes
DECOMPRESSION-BOMB DoS: 51049 bytes gzip → 52428800 bytes extracted (ratio 1027x).
kaniko has no size limit; a kilobyte-scale gzip can fill the build host's disk.
```

Reproducer: `pkg/util/repro_zipbomb_test.go` → `TestGzipBombNoSizeLimit`.

## Suggested fix

Wrap the layer reader in an `io.LimitReader` with a configurable max-extracted-size limit (e.g. `--max-extracted-size` flag, default a few GB). Refuse layers that exceed the limit:

```go
const defaultMaxExtractedSize = 10 << 30 // 10 GiB; CLI-overridable
r := io.LimitReader(layerReader, defaultMaxExtractedSize+1)
// after extraction, error if bytes read > defaultMaxExtractedSize
```

## Disclosure

Borderline — listed as DoS, comparable to crashes #2/#3/#5/#6/#7 already filed as public issues. Per `SECURITY.md` the conservative path is a private security advisory; the practical path matches what other DoS-via-malicious-base-image findings did.
