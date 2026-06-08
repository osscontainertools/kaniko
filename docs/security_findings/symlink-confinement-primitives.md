# Symlink-confinement primitives in ExtractFile (#9–#12)

**Severity:** HIGH — arbitrary file delete, read, symlink creation, and directory creation as root on the build host.
**Affects:** `pkg/util/fs_util.go` — `ExtractFile` (every TypeFlag branch that takes a `path` argument computed via `filepath.Join(dest, …)`) and the whiteout handler in `GetFSFromLayers`.
**Same root cause as `tar-slip-via-symlink.md` (no symlink validation in `ExtractFile`). Same fix (securejoin + `O_NOFOLLOW`).** But each is a distinct attack primitive that demonstrates a different class of damage, so they each deserve a test in the fix PR's regression suite.

Discovered by systematically probing every operation `ExtractFile` performs that takes a `path` argument. Each inherits the same vulnerability: a previously-planted symlink in the path causes the kernel to follow it during the syscall.

## #9 — Arbitrary file delete via whiteout + planted symlink

**Where:** `pkg/util/fs_util.go:212` (`os.RemoveAll(path)` in the whiteout handler in `GetFSFromLayers`).

**Trigger:**
```
SYMLINK   evil    →   /var/log/    (any existing host dir)
REGFILE   evil/.wh.audit.log       (whiteout for "audit.log")
```
Layer extraction computes `path := filepath.Join("dest/evil", "audit.log")` → resolves through symlink → `os.RemoveAll("/var/log/audit.log")`.

**Confirmed:** `TestWhiteoutArbitraryDeleteViaSymlink` — file written into `external/victim.txt` then deleted via whiteout entry.

```
ARBITRARY DELETE via whiteout+symlink: ".../victim.txt" was deleted outside dest
```

## #10 — Arbitrary host file read via hardlink + planted symlink

**Where:** `pkg/util/fs_util.go:409` (`os.Link(link, path)` in the `TypeLink` branch).

**Trigger:**
```
SYMLINK   evil    →   /etc
TypeLink  leak    →   evil/passwd        (Linkname resolves through planted symlink)
```
kaniko computes `link := filepath.Clean(filepath.Join(dest, "evil/passwd"))` → kernel `link()` follows the planted symlink → hardlink to `/etc/passwd` lands at `dest/leak`. The build now has a fully-readable alias of `/etc/passwd`, which can be COPY'd into the resulting image or read by any RUN step.

**Confirmed:** `TestHardlinkLeakViaSymlink` — `dest/leak` content reads back as `"CONFIDENTIAL"` (the external secret's content).

```
HARDLINK LEAK via planted symlink: dest/leak now mirrors ".../secret" (content="CONFIDENTIAL").
A malicious layer can read arbitrary host files into the build's filesystem.
```

## #11 — Arbitrary symlink creation outside dest via planted symlink

**Where:** `pkg/util/fs_util.go:426` (`os.Symlink(hdr.Linkname, path)` in the `TypeSymlink` branch — and its preceding `os.MkdirAll(dir, …)`).

**Trigger:**
```
SYMLINK   evil          →   /tmp/host-target
SYMLINK   evil/sneaky   →   /anything                (creates host-target/sneaky on the host)
```

**Confirmed:** `TestSymlinkChainCreatesSymlinkOutsideDest` — `external/sneaky` symlink created on the host with attacker-controlled `Linkname`. Stepping stone for further attacks during subsequent build steps.

## #12 — Arbitrary directory creation outside dest with attacker mode bits

**Where:** `pkg/util/fs_util.go:374-381` (`MkdirAllWithPermissions(path, mode, uid, gid)` and `os.Chmod(path, mode)` in the `TypeDir` branch).

**Trigger:**
```
SYMLINK   evil           →   /tmp/host-target
TypeDir   evil/newdir    mode=0o777
```
`MkdirAll` follows the planted symlink and creates `/tmp/host-target/newdir` with mode 0o777 (world-writable, attacker-chosen). With setuid/setgid bits in mode, even nastier.

**Confirmed:** `TestMkdirOutsideDestViaSymlink`:

```
DIRECTORY CREATED OUTSIDE DEST: ".../newdir" (mode=drwxrwxrwx) was created on the host via planted symlink
```

## Combined impact

All five primitives (the original tar-slip-write plus #9–#12) are reachable from any malicious base image layer tar with no path traversal in entry names (`../` is correctly blocked). The attacker only needs:

1. One symlink entry with `Linkname` set to an existing absolute path (or any target that resolves to one after subsequent layer extraction)
2. A subsequent entry whose `Name` contains that symlink's name as a directory component

| Primitive | Mechanism | Reference |
|---|---|---|
| File WRITE | TypeReg → `os.Create` follows symlink | `tar-slip-via-symlink.md` |
| File DELETE | Whiteout → `os.RemoveAll` follows symlink | #9 |
| File READ into build | TypeLink → `os.Link` follows symlink | #10 |
| Symlink CREATE outside | TypeSymlink → `os.Symlink`'s MkdirAll follows | #11 |
| Directory CREATE outside (attacker mode) | TypeDir → `MkdirAll`+`Chmod` follow | #12 |

## Single suggested fix

Wrap every path computation in `ExtractFile` (and the whiteout handler in `GetFSFromLayers`) with `securejoin.SecureJoin(dest, cleanedName)` — which resolves symlinks and rejects paths that escape `dest`. Plus open the regular-file write with `O_NOFOLLOW` as defense-in-depth.

```go
// Pseudocode for the fix:
safePath, err := securejoin.SecureJoin(dest, cleanedName)
if err != nil { return err }
// And for TypeSymlink: also validate the Linkname target wouldn't escape:
safeTarget, err := securejoin.SecureJoin(filepath.Dir(safePath), hdr.Linkname)
if err != nil { return fmt.Errorf("symlink target escapes dest: %w", err) }
// And for TypeReg writes:
f, err := os.OpenFile(safePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, mode)
```

The existing tests at `fs_util_test.go:875` and `:899` already encode the expected behavior using securejoin terminology in their comments — they just don't pass against the current production code.

## Disclosure

Not yet reported. Per `SECURITY.md`, file via GitHub security advisory at https://github.com/osscontainertools/kaniko/security/advisories — do not file as a public issue. Bundle with `tar-slip-via-symlink.md` (same root cause, same fix).
