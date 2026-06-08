# Bug #2594: Hardlinks lost during COPY --from

## Status
Confirmed bug present in `main`.

## Summary

`COPY --from=<stage>` silently breaks hardlinks. Files that shared an inode in the
source stage become independent regular files in the output image, inflating image size.
Docker/BuildKit preserves hardlinks correctly.

Real-world impact: `bitnami/git` has 141 hardlinks under `/opt/bitnami/git/libexec/git-core/`,
causing kaniko to produce a 720 MB image vs Docker's 83 MB.

## Reproduction

`integration/dockerfiles/Dockerfile_test_issue_2594` — self-contained reproducer.
Run with: `DOCKERFILE_TEST_FILTER=Dockerfile_test_issue_2594 bash scripts/integration-test.sh`

## Root Cause

### The break point: `CopyDir` in `pkg/util/fs_util.go:673`

`CopyDir` classifies each file as one of:

```
file == "."  →  MkdirAll
fi.IsDir()   →  MkdirAll
IsSymlink()  →  CopySymlink
else         →  CopyFile       ← hardlinks land here
```

`os.Lstat` returns hardlinks as regular files — there is no mode bit distinguishing them.
`CopyFile` (`fs_util.go:813`) opens the source and writes a new independent file, giving
it its own inode. The hardlink relationship is destroyed.

### Why the snapshot pass can't recover it

`checkHardlink` in `tar_util.go:194` detects hardlinks at snapshot time via `Nlink > 1`.
But `CopyFile` already created each file with a fresh inode (`Nlink == 1`), so
`checkHardlink` finds nothing and writes every file as a regular tar entry.

### Why Docker works

BuildKit processes `COPY --from` through tar streams end-to-end. `tar.TypeLink` entries
from the source layer are reproduced verbatim in the output — inodes are never
materialised, so the relationship is never lost.

### What already works

`ExtractFile` in `fs_util.go:376` correctly handles `tar.TypeLink` when extracting source
images to the inter-stage directory (`os.Link` is called for each hardlink entry). The
problem is only in the subsequent `CopyDir` step.

## Fix

### One change: add inode tracking to `CopyDir`

Add a `map[uint64]string` (inode → first dest path) before the file loop — the same
pattern `Tar.hardlinks` uses in `tar_util.go:40`.

In the `else` branch (`fs_util.go:732`), before calling `CopyFile`, extract
`syscall.Stat_t` via `getSyscallStatT` (already in `tar_util.go:214`, same `util`
package, no move needed):

```go
// first occurrence of this inode: copy normally, record dest
// later occurrences: create a hardlink to the first dest, skip CopyFile
hardlinksSeen := make(map[uint64]string)
```

```go
} else {
    if linked, linkDst := checkCopyHardlink(fi, destPath, hardlinksSeen); linked {
        if err := os.Link(linkDst, destPath); err != nil {
            return nil, err
        }
    } else {
        if _, err := CopyFile(fullPath, destPath, context, uid, gid, mode, useDefaultChmod); err != nil {
            return nil, err
        }
    }
}
```

```go
// checkCopyHardlink returns (true, existingDest) for the second and later occurrences
// of an inode. On first occurrence it records inode→dest and returns (false, "").
func checkCopyHardlink(fi os.FileInfo, dest string, seen map[uint64]string) (bool, string) {
    stat := getSyscallStatT(fi)
    if stat == nil || stat.Nlink <= 1 {
        return false, ""
    }
    if existing, ok := seen[stat.Ino]; ok {
        return true, existing
    }
    seen[stat.Ino] = dest
    return false, ""
}
```

### No other changes needed

- `ExtractFile` — already correct.
- `Tar.checkHardlink` — will automatically detect the now-preserved hardlinks
  (`Nlink > 1` again on the filesystem) and write correct `tar.TypeLink` entries.
- `CopyFile` — no change needed.

## Affected locations

| File | Lines | Change |
|------|-------|--------|
| `pkg/util/fs_util.go` | 673–754 | `CopyDir` — add `hardlinksSeen` map and hardlink branch |
| `pkg/util/fs_util.go` | — | add `checkCopyHardlink` helper |
| `pkg/util/tar_util.go` | 194–221 | `checkHardlink`, `getSyscallStatT` — no change |
