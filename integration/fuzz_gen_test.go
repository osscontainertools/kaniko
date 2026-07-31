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

package integration

import (
	"fmt"
	"io/fs"
	"strings"
)

// Pinned base images so generation does not depend on tag state. alpine is a
// single-layer docker v2 base and debian is multi-layer docker v2, so the mix
// exercises kaniko's base-layer preservation against different layer counts. The
// harness adds an OCI-media-type base at runtime (fuzzBaseRefs), and kaniko mirrors
// whatever media type the base carries, so both output formats get tested. All
// carry a POSIX shell for the RUN vocabulary.
const (
	alpineFuzzBase = "alpine@sha256:5ce5f501c457015c4b91f91a15ac69157d9b06f1a75cf9107bf2b62e0843983a"
	debianFuzzBase = "debian:12.10@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56"
)

// chaosFlags are internal FF_KANIKO_* toggles the chaos build flips at random (on or off)
// purely to exercise flag-gated branches for coverage. It is a coverage-only build with no
// output comparison, so non-output-neutral flips are fine. DISABLE_HTTP (blocks the local
// registry), RUN_VIA_TINI and COPY_AS_ROOT (the harness needs them for RUN/COPY) are left
// out so the chaos build runs far enough to cover something.
var chaosFlags = []string{
	"FF_KANIKO_BUILDKIT_ARG_ENV_PRECEDENCE", "FF_KANIKO_CACHE_LOOKAHEAD",
	"FF_KANIKO_CACHE_PROBE_AFTER_MISS", "FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS",
	"FF_KANIKO_CLEAN_KANIKO_DIR", "FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS",
	"FF_KANIKO_DEPRECATE_INTER_STAGE_RESTORE", "FF_KANIKO_IGNORE_CACHED_MANIFEST",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY", "FF_KANIKO_NO_PROPAGATE_ANNOTATIONS",
	"FF_KANIKO_OCI_SCRATCH_BASE", "FF_KANIKO_OCI_WARMER", "FF_KANIKO_PRESERVE_HARDLINKS",
	"FF_KANIKO_PRESERVE_MOUNTED_PATHS", "FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS",
	"FF_KANIKO_RESOLVE_CACHE_KEY", "FF_KANIKO_RUN_MOUNT_BIND", "FF_KANIKO_SCOPED_DOCKERIGNORE",
	"FF_KANIKO_SECUREJOIN_EXTRACTION", "FF_KANIKO_SKIP_RELABEL_RECOMPRESS",
	"FF_KANIKO_SKIP_WRITE_WHITEOUTS", "FF_KANIKO_UNTAR_SKIP_ROOT",
	"FF_KANIKO_VOLUME_SKIP_MKDIR", "FF_KANIKO_WARMER_CACHE_LOCK",
}

// fixedFuzzEpoch is a constant mtime used for generated context files and touch
// targets so that two builds of the same case see identical inputs.
const fixedFuzzEpoch = 1000000000

// source hands out deterministic decisions from an opaque byte stream. The same
// bytes always yield the same Dockerfile, so a case is reproducible from its seed.
// This is the input model native Go fuzzing uses, so the same generator can later
// be driven by a fuzz corpus without change.
type source struct {
	b []byte
	i int
}

func (s *source) next() byte {
	if len(s.b) == 0 {
		return 0
	}
	v := s.b[s.i%len(s.b)]
	s.i++
	return v
}

func (s *source) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(s.next()) % n
}

func (s *source) chance(oneIn int) bool {
	return s.intn(oneIn) == 0
}

func srcPick[T any](s *source, opts []T) T {
	return opts[s.intn(len(opts))]
}

type fileKind int

const (
	kindRegular fileKind = iota
	kindSymlink
	kindHardlink
	kindTar
	kindTarGz
)

// fileSpec is one context entry the harness writes before building.
type fileSpec struct {
	name    string
	kind    fileKind
	content string
	target  string // symlink target, for kindSymlink
	mode    fs.FileMode
}

// genResult is a generated case: a Dockerfile, the context it references, and the
// kaniko flags to build it with. The flags apply to every kaniko build in the case
// (fresh, cache, determinism) so the kaniko-vs-kaniko oracles stay comparable.
type genResult struct {
	dockerfile  string
	context     []fileSpec
	kanikoFlags []string
	// buildArgs are the --build-arg NAME=VALUE pairs the case declares, threaded to
	// docker as well so the parity oracle stays valid. The kaniko side gets them via
	// kanikoFlags. argNames are just the declared names, for a collision oracle to reuse.
	buildArgs []string
	argNames  []string
	// envFlags are output-neutral FF_KANIKO_* toggles (NAME=VALUE) applied as env
	// overrides to every kaniko build of the case, so the flags are part of the search
	// space without breaking the kaniko-vs-kaniko oracles.
	envFlags []string
	// cacheCompression, if set (zstd|gzip), is applied to both sides of the cache
	// comparison only, never the docker-compared fresh build.
	cacheCompression string
	// cacheLocal picks the cache backend for the populate-consume oracle: true routes it
	// to an on-disk OCI layout (--cache-repo=oci:/cache/layout, pkg/cache LayoutCache),
	// false to the registry. Backend choice must not change output, so the same oracle
	// applies either way.
	cacheLocal bool
	// target, if set, is the stage to build (--target). Passed to both docker and every
	// kaniko build so the comparison stays like-for-like; it also determines which
	// stage's base fixes the output media type.
	target string
	// labels are --label KEY=VALUE pairs set on the image config. Passed to both docker
	// and kaniko (kaniko via kanikoFlags), so the parity oracle checks kaniko writes the
	// same config labels docker does.
	labels []string
	// annotations are --annotation KEY=VALUE pairs set on the image manifest (OCI concept,
	// but both tools also write them on a docker v2 manifest). Passed to both tools so the
	// parity oracle checks kaniko writes the same manifest annotations docker does.
	annotations []string
	// usesSecret is set when the case declares a RUN --mount=type=secret, so both tools get
	// the --secret flag and the secret oracle scans the built image for the leaked value.
	usesSecret bool
	// chaosEnv is a random subset of internal FF_KANIKO_* toggles (on or off) for the
	// coverage-only chaos build; it is not applied to the output-checked builds.
	chaosEnv []string
}

// generate turns a byte source into a Dockerfile and its context. It does not try
// to guarantee the build succeeds: docker is the arbiter of validity. A Dockerfile
// both tools reject is sterile, and one docker builds but kaniko does not (or the
// reverse) is a build-outcome divergence, which is exactly a case worth finding.
// RUN still uses a deterministic vocabulary with no wall-clock or network dependence
// so that a diff between two successful builds is a defect, not build nondeterminism.
func generate(s *source, bases []string) genResult {
	var ctx []fileSpec
	var regulars []string
	nfiles := 1 + s.intn(3)
	for i := 0; i < nfiles; i++ {
		name := fmt.Sprintf("ctx%d", i)
		ctx = append(ctx, fileSpec{
			name:    name,
			content: fmt.Sprintf("fuzz-content-%d\n", i),
			// Includes setuid, setgid, and sticky bits to exercise how the tar path
			// preserves special bits, an area diffoci compares as a mode diff. Modes
			// without owner-read are excluded: the buildkit client runs as the invoking
			// user and cannot read them, while kaniko reads the mounted context as root,
			// so they produce a context-access artifact rather than a build divergence.
			mode: srcPick(s, []fs.FileMode{
				0o644, 0o600, 0o755, 0o444, 0o777,
				fs.ModeSetuid | 0o755, fs.ModeSetgid | 0o750, fs.ModeSticky | 0o777,
			}),
		})
		regulars = append(regulars, name)
	}
	// A symlink into the context, to exercise how COPY treats a symlinked source.
	symName := ""
	if s.chance(2) {
		symName = "linkS"
		ctx = append(ctx, fileSpec{name: symName, kind: kindSymlink, target: regulars[0]})
	}
	// A tar in the context, to exercise ADD auto-extraction. Sometimes gzip-compressed
	// (.tar.gz), which ADD also auto-extracts through kaniko's decompression path.
	tarName := ""
	if s.chance(2) {
		if s.chance(2) {
			tarName = "arc.tar.gz"
			ctx = append(ctx, fileSpec{name: tarName, kind: kindTarGz})
		} else {
			tarName = "arc.tar"
			ctx = append(ctx, fileSpec{name: tarName, kind: kindTar})
		}
	}
	// A hardlink into the context, to exercise how COPY treats hardlinked sources.
	hlinkName := ""
	if s.chance(2) {
		hlinkName = "hlink"
		ctx = append(ctx, fileSpec{name: hlinkName, kind: kindHardlink, target: regulars[0]})
	}
	// A context subdirectory, to exercise a directory COPY (the CopyDir path).
	dirName := ""
	if s.chance(2) {
		dirName = "d0"
		ctx = append(ctx,
			fileSpec{name: "d0/f0", content: "d0-f0\n", mode: 0o644},
			fileSpec{name: "d0/f1", content: "d0-f1\n", mode: 0o600})
	}
	// A source with a space in its name, to exercise odd-path handling.
	oddName := ""
	if s.chance(2) {
		oddName = "odd name.txt"
		ctx = append(ctx, fileSpec{name: oddName, content: "odd\n", mode: 0o644})
	}
	// A .dockerignore, sometimes, to exercise ignore matching against a full-context COPY
	// (FF_KANIKO_SCOPED_DOCKERIGNORE). docker and kaniko must select the same files. It
	// always excludes build-meta so the COPY does not pull the Dockerfile or the tar, plus
	// one fuzzed pattern (exact name, glob, dir, or nested) to test the matcher.
	hasIgnore := false
	if s.chance(2) {
		hasIgnore = true
		// Dedicated ignorable files, so an exclusion never collides with a directly-COPY'd
		// ctx file (which makes docker fail while kaniko copies it, a separate divergence
		// that would flood). Patterns exercise glob and negation matching against COPY .
		ctx = append(ctx,
			fileSpec{name: "ignoreme", content: "ign\n", mode: 0o644},
			fileSpec{name: "keepme", content: "keep\n", mode: 0o644})
		ig := "Dockerfile\n.dockerignore\n.buildctx.tar.gz\n*me\n!keepme\n"
		ctx = append(ctx, fileSpec{name: ".dockerignore", content: ig, mode: 0o644})
	}

	uids := []string{"0", "1000", "65534"}
	// user/group tokens for --chown and USER: numeric plus names present in both alpine and
	// debian (root, bin, daemon), so a named owner resolves from the base's /etc/passwd and
	// /etc/group. Kept separate from uids, which also feed numeric-only mount uid= fields.
	ug := []string{"0", "1000", "65534", "root", "bin", "daemon"}
	modes := []string{"0644", "0600", "0755", "0700", "0777"}
	stopsignals := []string{"SIGTERM", "SIGKILL", "9", "SIGQUIT"}

	var b strings.Builder
	var buildArgs []string
	var argNames []string
	var namedStages []int // earlier non-final stages, candidates for FROM inheritance
	// Optionally declare a global base ARG so a stage can use a build-arg base
	// (FROM $SBASE). It must precede the first FROM; docker only allows pre-FROM ARGs in
	// FROM. Its default is a real base so finalBaseRef can resolve the media type.
	baseArg := ""
	if s.chance(3) {
		baseArg = "SBASE"
		fmt.Fprintf(&b, "ARG %s=%s\n", baseArg, srcPick(s, bases))
		// Sometimes override the default at build time (dynamic base), threaded to both
		// docker and kaniko via buildArgs so FROM $SBASE resolves to the overridden base.
		if s.chance(2) {
			buildArgs = append(buildArgs, baseArg+"="+srcPick(s, bases))
		}
	}
	// Sometimes build from an empty base (FROM scratch), which has no shell or base
	// filesystem, so the stage carries only file and metadata instructions, no RUN. A
	// scratch build's output media type is governed by FF_KANIKO_OCI_SCRATCH_BASE, tested
	// both on and off; docker is told to match whichever kaniko will emit. Single stage,
	// to keep the empty-base path self-contained.
	scratch := s.chance(5)
	scratchOCI := false
	if scratch {
		scratchOCI = s.chance(2)
	}
	// Set when a generated RUN --mount=type=secret appears, so both tools get the --secret
	// flag and the secret-leak oracle runs on the built image.
	usesSecret := false
	nstages := 1 + s.intn(4)
	if scratch {
		nstages = 1
	}
	for stage := 0; stage < nstages; stage++ {
		last := stage == nstages-1
		// Base is normally an external image. For a non-first stage, sometimes inherit an
		// earlier named stage instead (FROM stageJ), which builds on its filesystem and
		// fires any ONBUILD triggers that stage registered (pkg/commands/onbuild.go).
		base := srcPick(s, bases)
		if scratch {
			base = "scratch"
		} else if len(namedStages) > 0 && s.chance(2) {
			base = fmt.Sprintf("stage%d", namedStages[s.intn(len(namedStages))])
		} else if baseArg != "" && s.chance(2) {
			base = "$" + baseArg // build-arg base (FROM $SBASE)
		}
		if last {
			fmt.Fprintf(&b, "FROM %s\n", base)
			// A full-context COPY makes .dockerignore observable: docker and kaniko must
			// copy the same file set. Only when a .dockerignore is present, so it excludes
			// build-meta and the copied set is well-defined.
			if hasIgnore {
				fmt.Fprintf(&b, "COPY . /allctx/\n")
			}
			// Declare a few build args, reference them in one RUN, and pass generated
			// values. This exercises --build-arg parsing (pkg/config), env replacement,
			// and the build-arg cache key. Values are drawn from a pool biased toward
			// delimiter-heavy strings ("x-FB=z"), the shape that collides in the joined
			// composite cache key (mz873), so the corpus explores that surface.
			if !scratch && s.chance(2) {
				pool := []string{"FA", "FB", "FC"}
				nargs := 2 + s.intn(2)
				argNames = pool[:nargs]
				var refs strings.Builder
				for _, n := range argNames {
					fmt.Fprintf(&b, "ARG %s\n", n)
					fmt.Fprintf(&refs, "%s=[$%s] ", n, n)
				}
				fmt.Fprintf(&b, "RUN echo \"%s\" > /argout\n", strings.TrimSpace(refs.String()))
				for _, n := range argNames {
					v := argValue(s, argNames)
					buildArgs = append(buildArgs, n+"="+v)
				}
			}
		} else {
			// A non-final stage is named and exports an artifact so a later stage
			// has a real built file to COPY --from, exercising cross-stage ownership
			// and timestamp handling rather than just copying a base-image file.
			fmt.Fprintf(&b, "FROM %s AS stage%d\n", base, stage)
			fmt.Fprintf(&b, "RUN mkdir -p /stage && echo stage%d > /stage/artifact\n", stage)
			// Register an ONBUILD trigger that fires when a later stage does FROM this one
			// (pkg/commands/onbuild.go). Cover exec triggers (RUN/COPY) and metadata
			// triggers (LABEL/ENV), which take different handling paths.
			if s.chance(2) {
				switch s.intn(4) {
				case 0:
					fmt.Fprintf(&b, "ONBUILD RUN mkdir -p /onbuild && echo ob%d > /onbuild/m%d\n", stage, stage)
				case 1:
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "ONBUILD COPY %s /onbuild/%s\n", f, f)
				case 2:
					fmt.Fprintf(&b, "ONBUILD LABEL onbuild.stage%d=v%d\n", stage, stage)
				case 3:
					fmt.Fprintf(&b, "ONBUILD ENV ONBUILD_ENV_%d=v%d\n", stage, stage)
				}
			}
			namedStages = append(namedStages, stage)
		}

		// Cross-stage COPYs form the dependency graph. A stage may copy from several
		// earlier stages (a DAG, not just a chain), referenced by name or by numeric
		// index, sometimes with --chown. This stresses the cross-stage cache-key
		// inference and the shared snapshotter, where the recent bugs cluster
		// (mz334/mz782/mz872/mz876).
		for j := 0; j < stage; j++ {
			if !s.chance(2) {
				continue
			}
			ref := fmt.Sprintf("stage%d", j)
			if s.chance(2) {
				ref = fmt.Sprintf("%d", j) // numeric stage index, a distinct resolution path
			}
			opt := ""
			if s.chance(3) {
				opt = fmt.Sprintf(" --chown=%s:%s", srcPick(s, ug), srcPick(s, ug))
			}
			fmt.Fprintf(&b, "COPY --from=%s%s /stage/artifact /dest/from-%d\n", ref, opt, j)
		}

		ninstr := 2 + s.intn(6)
		for i := 0; i < ninstr; i++ {
			c := s.intn(34)
			// An empty base has no shell, so RUN-based instructions cannot execute at build
			// time; remap them to a context COPY, which scratch supports. Metadata and other
			// file instructions are already scratch-safe (ADD url downloads into the empty fs).
			if scratch && (c == 10 || c == 11 || c == 12 || c == 20 || c == 28 || c == 29 || c == 30 || c == 32 || c == 33) {
				c = 0
			}
			switch c {
			case 0, 1, 2:
				// COPY a context file, sometimes with --chown or --chmod, the areas
				// where docker and kaniko most often disagree on ownership and mode.
				// Left unconstrained under USER on purpose: kaniko copies as root
				// (FF_KANIKO_COPY_AS_ROOT) where docker fails, a build-outcome divergence.
				f := srcPick(s, regulars)
				var opts string
				if s.chance(2) {
					opts += fmt.Sprintf(" --chown=%s:%s", srcPick(s, ug), srcPick(s, ug))
				}
				if s.chance(2) {
					opts += fmt.Sprintf(" --chmod=%s", srcPick(s, modes))
				}
				fmt.Fprintf(&b, "COPY%s %s /dest/%s\n", opts, f, f)
			case 3:
				// ADD a plain file, which behaves like COPY.
				f := srcPick(s, regulars)
				fmt.Fprintf(&b, "ADD %s /added/%s\n", f, f)
			case 4:
				// ADD a tar, which docker and kaniko auto-extract.
				if tarName != "" {
					fmt.Fprintf(&b, "ADD %s /extract%d/\n", tarName, i)
				} else {
					fmt.Fprintf(&b, "ENV FUZZ_KEY_%d=value%d\n", i, i)
				}
			case 5:
				// COPY a symlinked source.
				if symName != "" {
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", symName, symName)
				} else {
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", f, f)
				}
			case 6:
				fmt.Fprintf(&b, "ENV FUZZ_KEY_%d=value%d\n", i, i)
			case 7:
				// Absolute or relative WORKDIR. A relative path resolves against the current
				// working dir (the previous WORKDIR or /), exercising that resolution and the
				// implicit-dir creation both tools must agree on.
				if s.chance(2) {
					fmt.Fprintf(&b, "WORKDIR /work/dir%d\n", i)
				} else {
					fmt.Fprintf(&b, "WORKDIR rel%d\n", i)
				}
			case 8:
				fmt.Fprintf(&b, "LABEL fuzz.label.%d=v%d\n", i, i)
			case 9:
				fmt.Fprintf(&b, "EXPOSE %d\n", 8000+i)
			case 10:
				// Deterministic RUN: create a dir and a file with a fixed mtime and mode.
				fmt.Fprintf(&b, "RUN mkdir -p /r/dir%d && touch -d @%d /r/dir%d/file%d && chmod %s /r/dir%d/file%d\n",
					i, fixedFuzzEpoch, i, i, srcPick(s, modes), i, i)
			case 11:
				// Deterministic RUN: create then remove a symlink.
				fmt.Fprintf(&b, "RUN mkdir -p /r && ln -sf /etc/hostname /r/link%d && rm -f /r/link%d\n", i, i)
			case 12:
				// Delete a builtin file, exercising whiteout handling on base content.
				fmt.Fprintf(&b, "RUN rm -f /etc/os-release\n")
			case 13:
				fmt.Fprintf(&b, "USER %s\n", srcPick(s, ug))
			case 14:
				if s.chance(2) {
					fmt.Fprintf(&b, "ENTRYPOINT [\"/bin/echo\", \"hi%d\"]\n", i)
				} else {
					fmt.Fprintf(&b, "ENTRYPOINT /bin/echo hi%d\n", i)
				}
			case 15:
				if s.chance(2) {
					fmt.Fprintf(&b, "CMD [\"/bin/echo\", \"cmd%d\"]\n", i)
				} else {
					fmt.Fprintf(&b, "CMD /bin/echo cmd%d\n", i)
				}
			case 16:
				fmt.Fprintf(&b, "VOLUME /vol%d\n", i)
			case 17:
				fmt.Fprintf(&b, "STOPSIGNAL %s\n", srcPick(s, stopsignals))
			case 18:
				fmt.Fprintf(&b, "SHELL [\"/bin/sh\", \"-c\"]\n")
			case 19:
				if s.chance(3) {
					fmt.Fprintf(&b, "HEALTHCHECK NONE\n")
				} else {
					fmt.Fprintf(&b, "HEALTHCHECK CMD /bin/true\n")
				}
			case 20:
				// ARG with a default, made observable by writing it to a file.
				fmt.Fprintf(&b, "ARG FUZZ_ARG_%d=arg%d\nRUN echo $FUZZ_ARG_%d > /arg%d\n", i, i, i, i)
			case 21:
				// Glob COPY of every context file.
				fmt.Fprintf(&b, "COPY ctx* /glob%d/\n", i)
			case 22:
				// Multi-source COPY (needs a trailing-slash dir dest).
				if len(regulars) >= 2 {
					fmt.Fprintf(&b, "COPY %s %s /multi%d/\n", regulars[0], regulars[1], i)
				} else {
					fmt.Fprintf(&b, "ENV FUZZ_KEY_%d=value%d\n", i, i)
				}
			case 23:
				// Directory COPY, exercising the CopyDir path and its implicit-dir chmod.
				if dirName != "" {
					fmt.Fprintf(&b, "COPY %s /dircopy%d/\n", dirName, i)
				} else {
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", f, f)
				}
			case 24:
				// COPY a hardlinked source.
				if hlinkName != "" {
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", hlinkName, hlinkName)
				} else {
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", f, f)
				}
			case 25:
				// COPY --from an external image, exercising cross-image copy.
				fmt.Fprintf(&b, "COPY --from=%s /etc/hostname /ext%d\n", srcPick(s, bases), i)
			case 26:
				// COPY a source whose name has a space, using the exec form.
				if oddName != "" {
					fmt.Fprintf(&b, "COPY [%q, %q]\n", oddName, fmt.Sprintf("/dest/oddname%d", i))
				} else {
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "COPY %s /dest/%s\n", f, f)
				}
			case 27:
				// ONBUILD in any stage, including the always-built final one, so
				// OnBuildCommand is processed (pkg/commands/onbuild.go) whether or not a
				// descendant later fires it. Both tools record it in the image config, so
				// a divergence in how the trigger is stored is a metadata finding.
				switch s.intn(3) {
				case 0:
					fmt.Fprintf(&b, "ONBUILD RUN echo onbuild%d > /ob%d\n", i, i)
				case 1:
					fmt.Fprintf(&b, "ONBUILD LABEL ob.k%d=v%d\n", i, i)
				case 2:
					f := srcPick(s, regulars)
					fmt.Fprintf(&b, "ONBUILD COPY %s /ob/%s\n", f, f)
				}
			case 28:
				// Heredoc forms, which docker and kaniko both support. COPY heredoc writes
				// inline content to a file; RUN heredoc runs a multi-line script.
				if s.chance(2) {
					fmt.Fprintf(&b, "COPY <<EOF /heredoc/f%d\nheredoc-content-%d\nEOF\n", i, i)
				} else {
					fmt.Fprintf(&b, "RUN <<EOF\nmkdir -p /hd%d\necho run-heredoc-%d > /hd%d/out\nEOF\n", i, i, i)
				}
			case 29:
				// RUN --mount=type=bind: a context file is mounted into the RUN but is
				// ephemeral, so it must not appear in the layer. Both tools support it
				// (kaniko via FF_KANIKO_RUN_MOUNT_BIND), a good test of mount-vs-layer.
				f := srcPick(s, regulars)
				fmt.Fprintf(&b, "RUN --mount=type=bind,source=%s,target=/mnt/%s cat /mnt/%s > /mnt-out%d\n", f, f, f, i)
			case 30:
				// umask and explicit-mode RUN. Created file and dir modes must reflect the
				// umask, and chmod must set special bits (setuid/setgid/sticky) that survive
				// the snapshot. A docker-vs-kaniko mode divergence here is a real finding.
				if s.chance(2) {
					um := srcPick(s, []string{"077", "022", "002", "027", "000"})
					fmt.Fprintf(&b, "RUN umask %s && mkdir -p /um%d/d && touch /um%d/d/f && echo x > /um%d/g\n", um, i, i, i)
				} else {
					mode := srcPick(s, []string{"4755", "2755", "1777", "0640", "0600"})
					fmt.Fprintf(&b, "RUN touch /sm%d && chmod %s /sm%d && mkdir -p /smd%d && chmod %s /smd%d\n", i, mode, i, i, mode, i)
				}
			case 31:
				// ADD a remote URL, exercising kaniko's download path (DownloadFileToDest).
				// Both tools fetch the campaign's local file server; a --chown/--chmod
				// sometimes overrides the default 0600 both apply to a downloaded file.
				opt := ""
				if s.chance(2) {
					opt = fmt.Sprintf(" --chown=%s:%s", srcPick(s, ug), srcPick(s, ug))
				}
				if s.chance(2) {
					opt += fmt.Sprintf(" --chmod=%s", srcPick(s, modes))
				}
				fmt.Fprintf(&b, "ADD%s %s /urlget%d/addfile\n", opt, addURL, i)
			case 32:
				// RUN --mount=type=cache: an ephemeral cache mount (swapDir), whose content
				// must not appear in the layer. Write into it and read back to a real file, so
				// the layer captures the result but never the mount. Optional mode/uid and a
				// cache id exercise the chmod/chown and keyed-cache branches; all ephemeral, so
				// docker and kaniko must agree the mount leaves no trace.
				opt := ""
				if s.chance(2) {
					opt += fmt.Sprintf(",mode=%s,uid=%s", srcPick(s, modes), srcPick(s, uids))
				}
				if s.chance(3) {
					opt += fmt.Sprintf(",id=cacheid%d", i)
				}
				fmt.Fprintf(&b, "RUN --mount=type=cache,target=/cache%d%s echo cache-data-%d > /cache%d/f && cat /cache%d/f > /cout%d\n", i, opt, i, i, i, i)
			case 33:
				// RUN --mount=type=secret: consume a build secret through an ephemeral mount
				// and persist only its byte length, never its value. The secret oracle then
				// asserts the value is absent from every layer and the config. Both tools get
				// the same secret so the length matches; the file and env delivery forms take
				// different handler branches.
				usesSecret = true
				if s.chance(2) {
					fmt.Fprintf(&b, "RUN --mount=type=secret,id=%s,env=SEC%d printf '%%s' \"$SEC%d\" | wc -c > /seclen%d\n", secretID, i, i, i)
				} else {
					fmt.Fprintf(&b, "RUN --mount=type=secret,id=%s,target=/sec%d wc -c < /sec%d > /seclen%d\n", secretID, i, i, i)
				}
			}
		}
	}

	// Flag variety, drawn from the same byte source. These are output-neutral for
	// the docker oracle (the final image should match docker regardless), except
	// --single-snapshot, which squashes layers; buildAndClassify relaxes the docker
	// layer-count comparison when it is present. All exercise flag-gated code and
	// config parsing that a fixed flag set never reaches.
	var flags []string
	switch s.intn(3) {
	case 1:
		flags = append(flags, "--snapshot-mode=redo")
	case 2:
		flags = append(flags, "--snapshot-mode=time")
	}
	if s.chance(4) {
		flags = append(flags, "--single-snapshot")
	}
	if s.chance(3) {
		flags = append(flags, "--compressed-caching=false")
	}
	// More output-neutral flags: the final image is unchanged, so all oracles stay
	// valid. --cache-run-layers toggles RUN-layer caching (cache-side), --use-new-run
	// selects the experimental snapshotless run implementation (RunV2).
	if s.chance(4) {
		flags = append(flags, "--cache-run-layers=false")
	}
	if s.chance(4) {
		flags = append(flags, "--use-new-run")
	}
	// --materialize forces the final filesystem to be unpacked even on a full cache hit,
	// and --cleanup wipes the working filesystem after the build. Both leave the built
	// image unchanged, so they belong in the output-neutral flag pool.
	if s.chance(4) {
		flags = append(flags, "--materialize")
	}
	if s.chance(4) {
		flags = append(flags, "--cleanup")
	}
	// Compression is tested only around caching. On an OCI base it changes the layer
	// media type (tar+zstd vs docker's tar+gzip), which diffoci flags as a descriptor
	// diff, so it must not touch the docker-compared fresh build. Applied to both sides
	// of the cache comparison, it exercises how cache layers are stored and read back.
	cacheCompression := ""
	switch s.intn(3) {
	case 1:
		cacheCompression = "zstd"
	case 2:
		cacheCompression = "gzip"
	}

	// Feature-flag search space: output-neutral FF_KANIKO_* toggles drawn from the byte
	// source and applied to every kaniko build of the case (fresh, cache, determinism,
	// warmer), so all oracles exercise them. They must not change the built image; a
	// divergence is a real bug. Only flags verified output-neutral versus docker go here,
	// so the docker-parity oracle stays valid. These are cache-focused, where the recent
	// bugs clustered (mz872/mz873/mz876). Add more as they are verified.
	var envFlags []string
	for _, ff := range []string{
		"FF_KANIKO_CACHE_PROBE_AFTER_MISS",   // after a miss, keep probing later layers
		"FF_KANIKO_IGNORE_CACHED_MANIFEST",   // do not short-circuit on a cached manifest
		"FF_KANIKO_ROLLING_CACHE_KEY",        // recursive-hash composite cache key (mz873)
		"FF_KANIKO_CLEAN_KANIKO_DIR",         // wipe /kaniko after build, image unaffected
		"FF_KANIKO_SKIP_RELABEL_RECOMPRESS",  // relabel a converted layer without recompress; digest-asserted equal
		"FF_KANIKO_DISABLE_HTTP2",            // registry transport over HTTP/1.1, image unaffected
		"FF_KANIKO_SHARED_BASE_CACHE",        // dedup base-image downloads across stages (mz936), image unaffected
		"FF_KANIKO_SKIP_CACHED_STAGES",       // squash fully cached stages after lookahead (mz334), final image unaffected
	} {
		if s.chance(2) {
			envFlags = append(envFlags, ff+"=1")
		}
	}
	// For a scratch build, pin the empty-base output media type explicitly and test both
	// values. buildAndClassify reads this back to tell docker which media type to match.
	if scratch {
		v := "0"
		if scratchOCI {
			v = "1"
		}
		envFlags = append(envFlags, "FF_KANIKO_OCI_SCRATCH_BASE="+v)
	}

	// Sometimes build only up to an earlier named stage (--target), exercising stage
	// pruning. Passed to every kaniko build via kanikoFlags; the docker side gets it via
	// genResult.target. The target stage's base fixes the output media type.
	target := ""
	if len(namedStages) > 0 && s.chance(3) {
		target = fmt.Sprintf("stage%d", namedStages[s.intn(len(namedStages))])
		flags = append(flags, "--target="+target)
	}

	// Pass the build args to every kaniko build via kanikoFlags so the fresh, cache,
	// determinism, invariance, and warmer builds all see identical args. The docker
	// side gets the same list through genResult.buildArgs.
	for _, kv := range buildArgs {
		flags = append(flags, "--build-arg="+kv)
	}

	// Image labels: --label KEY=VALUE set on the config, passed to both tools so the parity
	// oracle checks kaniko writes the same labels docker does. Values reuse the build-arg
	// pool, which includes strings with '=' and dashes, stressing the SplitN key=value parse.
	var labels []string
	if s.chance(2) {
		n := 1 + s.intn(2)
		for i := 0; i < n; i++ {
			labels = append(labels, fmt.Sprintf("lbl%d=%s", i, argValue(s, argNames)))
		}
		for _, kv := range labels {
			flags = append(flags, "--label="+kv)
		}
	}

	// Image manifest annotations: --annotation KEY=VALUE, passed to both tools. Values are
	// kept simple (no embedded '=') so the check targets annotation propagation, not the
	// key=value parse the labels already stress.
	var annotations []string
	if s.chance(2) {
		n := 1 + s.intn(2)
		for i := 0; i < n; i++ {
			annotations = append(annotations, fmt.Sprintf("ann%d=%s", i, srcPick(s, []string{"v", "1.2.3", "a-b-c"})))
		}
		for _, kv := range annotations {
			flags = append(flags, "--annotation="+kv)
		}
	}

	// A case that uses a build secret gets the --secret flag on every kaniko build; the
	// docker side gets it via genResult.usesSecret. The value itself rides in KanikoEnv.
	if usesSecret {
		flags = append(flags, secretFlag)
	}

	// Route the cache oracle to the on-disk OCI-layout backend on a third of cases so the
	// LayoutCache read/write path is exercised alongside the registry backend. The backend
	// must not change output, so the populate-consume oracle is identical either way. Drawn
	// last so it does not perturb the byte positions the Dockerfile, target, and base
	// resolution depend on (a mid-stream draw shifts them and breaks the docker oracle).
	cacheLocal := s.chance(3)

	// Chaos flags: a random subset of internal FF_KANIKO_* toggles set on or off, for the
	// coverage-only chaos build (FUZZ_CHAOSFLAGS). Drawn last so it does not perturb the
	// byte positions the output-checked builds depend on.
	var chaosEnv []string
	for _, ff := range chaosFlags {
		if s.chance(2) {
			v := "0"
			if s.chance(2) {
				v = "1"
			}
			chaosEnv = append(chaosEnv, ff+"="+v)
		}
	}

	return genResult{dockerfile: b.String(), context: ctx, kanikoFlags: flags, buildArgs: buildArgs, argNames: argNames, envFlags: envFlags, cacheCompression: cacheCompression, cacheLocal: cacheLocal, target: target, labels: labels, annotations: annotations, usesSecret: usesSecret, chaosEnv: chaosEnv}
}

// argValue returns a build-arg value drawn from a pool biased toward strings that stress
// the composite cache key. The last case emits a value containing "-<name>=", the
// delimiter-overlap shape that made two distinct arg sets join to the same cache key in
// mz873, so the fuzzer exercises that surface rather than only benign values.
func argValue(s *source, names []string) string {
	switch s.intn(6) {
	case 0:
		return "v"
	case 1:
		return "1.2.3"
	case 2:
		return "a-b-c"
	case 3:
		return "--opt=1"
	case 4:
		return "k=v"
	default:
		// A value that collides with another arg name in the joined composite cache key
		// (mz873). With no other names to borrow, fall back to a plain delimiter-heavy value.
		if len(names) == 0 {
			return "x-y=z"
		}
		return fmt.Sprintf("x-%s=z", srcPick(s, names))
	}
}
