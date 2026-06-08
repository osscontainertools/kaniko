# Tar-slip via symlink: arbitrary file write during image unpacking

**Severity:** HIGH — arbitrary file write as root on the build host.
**Affects:** `pkg/util/fs_util.go` — `ExtractFile` (TypeSymlink branch, no Linkname validation) and the surrounding loop in `GetFSFromImage` / `UnTar`.
**Reproducible:** Yes, 100%.
**Pattern:** classic *tar slip via symlink* (CVE-2019-14271-class).

## Exploit recipe

A malicious base image's layer tar contains:

```
SYMLINK   evil   →   /tmp           (Typeflag = SYMTYPE, any *existing* abs path)
REGFILE   evil/PWNED   "content"    (Typeflag = REGTYPE)
```

When kaniko extracts the layer:

1. `ExtractFile` for the symlink: `os.Symlink("/tmp", dest+"/evil")` — no validation of Linkname.
2. `ExtractFile` for the regular file:
   - computes `path = dest + "/evil/PWNED"`
   - `os.Stat(dir)` follows the symlink to `/tmp/` — succeeds (target exists)
   - `os.Create(path)` follows the symlink during write — writes `/tmp/PWNED` on the build host.

Demonstrated end-to-end: a layer tar containing `(symlink "evil" → "/tmp", file "evil/PWNED_BY_KANIKO_FUZZ")` fed to `util.UnTar` results in `/tmp/PWNED_BY_KANIKO_FUZZ` existing after the call returns — outside the destination directory.

## Reproduce

```bash
go test ./pkg/util/ -run='TestTarSlipViaSymlinkEscape' -v
go test ./pkg/util/ -run='TestTarSlipVariants' -v
```

Output of the variants test:

```
=== RUN   TestTarSlipVariants/absolute_target
    ESCAPE: file written outside dest in external: "/tmp/.../leaked.txt"
--- FAIL: TestTarSlipVariants/absolute_target
--- PASS: TestTarSlipVariants/parent_traversal_symlink   (linkname=../escaped — target doesn't exist before)
--- PASS: TestTarSlipVariants/deep_traversal_symlink     (same)
```

Key insight from the variants: the escape fires when the symlink target is an existing absolute path. Targets like `../escaped` (non-existent) trip `MkdirAll` on the symlink and the write fails. But the attacker can pick any pre-existing path — `/`, `/etc`, `/tmp`, `/usr/local/bin`, `/kaniko/`, …

## Production impact

Kaniko's executor runs as uid 0 inside the build container, with the host filesystem mounted as the build root. A malicious base image can therefore:

- Overwrite `/etc/passwd`, `/etc/shadow`, `/etc/ssh/...`
- Plant a malicious binary at `/usr/local/bin/<name>` to shadow legitimate system tools
- Overwrite `/kaniko/executor` itself, affecting subsequent stages / cached builds
- Modify `/proc/sys/...` to alter kernel-level behavior
- Anywhere a path stat()s successfully and the process has write perms

The two vulnerable entry points cover both image extraction and Dockerfile-driven tar handling:

| Caller | Function | Reach |
|---|---|---|
| Base image extraction | `GetFSFromImage` → `ExtractFile` | Any `FROM <malicious-image>` |
| COPY/ADD local tar | `UnpackLocalTarArchive` → `UnTar` → `ExtractFile` | Any user-provided context tar |

## Root cause

```go
// pkg/util/fs_util.go, ExtractFile, TypeSymlink branch:
case tar.TypeSymlink:
    ...
    if err := os.Symlink(hdr.Linkname, path); err != nil {
        return err
    }
```

`hdr.Linkname` is written verbatim with no path-prefix check. Compare to `TypeLink` (hardlink) where there IS a `cleanedLink == ".." || strings.HasPrefix("../")` check — but that check only catches relative-parent escapes; an absolute target like `/etc` still slips through (it just produces an `os.Link` to a non-existent path under dest, so the hardlink fails harmlessly). The symlink path doesn't even have the relative-parent check.

## Suggested fix

Resolve symlinks during extraction with a chroot-like guard. The simplest variant:

```go
case tar.TypeSymlink:
    // Refuse absolute targets.
    if filepath.IsAbs(hdr.Linkname) {
        return fmt.Errorf("symlink %q has absolute target %q; refusing", hdr.Name, hdr.Linkname)
    }
    // Resolve the link's intended target relative to its containing directory
    // and refuse anything that escapes dest.
    abs := filepath.Clean(filepath.Join(filepath.Dir(path), hdr.Linkname))
    if !strings.HasPrefix(abs+string(os.PathSeparator), dest+string(os.PathSeparator)) && abs != dest {
        return fmt.Errorf("symlink %q target %q escapes %q", hdr.Name, hdr.Linkname, dest)
    }
    if err := os.Symlink(hdr.Linkname, path); err != nil {
        return err
    }
```

Additionally, the regular-file write path should `os.OpenFile(path, O_NOFOLLOW|O_CREATE|O_WRONLY|O_TRUNC, ...)` (or equivalent on Linux) so that even if a symlink escapes the symlink-creation check, the subsequent file write won't follow it. Defense-in-depth: validate both the link creation and the file open.

Tools like `securejoin.SecureJoin` (`github.com/cyphar/filepath-securejoin`) exist specifically for this. Recommend adopting it across `ExtractFile`, `UnTar`, `GetFSFromLayers`, and any other code that takes attacker-influenced relative paths.

## Related

See `symlink-confinement-primitives.md` — four additional attack primitives (file delete, file read, symlink creation, directory creation outside dest) share this same root cause and can be fixed together.

## Disclosure

Not yet reported. Per `SECURITY.md`, file via GitHub security advisory at https://github.com/osscontainertools/kaniko/security/advisories — do not file as a public issue.
