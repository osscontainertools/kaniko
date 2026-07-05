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
	// A tar in the context, to exercise ADD auto-extraction.
	tarName := ""
	if s.chance(2) {
		tarName = "arc.tar"
		ctx = append(ctx, fileSpec{name: tarName, kind: kindTar})
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

	uids := []string{"0", "1000", "65534"}
	modes := []string{"0644", "0600", "0755", "0700", "0777"}
	stopsignals := []string{"SIGTERM", "SIGKILL", "9", "SIGQUIT"}

	var b strings.Builder
	nstages := 1 + s.intn(3)
	for stage := 0; stage < nstages; stage++ {
		last := stage == nstages-1
		base := srcPick(s, bases)
		if last {
			fmt.Fprintf(&b, "FROM %s\n", base)
		} else {
			// A non-final stage is named and exports an artifact so a later stage
			// has a real built file to COPY --from, exercising cross-stage ownership
			// and timestamp handling rather than just copying a base-image file.
			fmt.Fprintf(&b, "FROM %s AS stage%d\n", base, stage)
			fmt.Fprintf(&b, "RUN mkdir -p /stage && echo stage%d > /stage/artifact\n", stage)
		}

		if stage > 0 && s.chance(2) {
			j := s.intn(stage) // an earlier, therefore non-final, named stage
			fmt.Fprintf(&b, "COPY --from=stage%d /stage/artifact /dest/from-stage%d\n", j, j)
		}

		ninstr := 2 + s.intn(6)
		for i := 0; i < ninstr; i++ {
			switch s.intn(27) {
			case 0, 1, 2:
				// COPY a context file, sometimes with --chown or --chmod, the areas
				// where docker and kaniko most often disagree on ownership and mode.
				// Left unconstrained under USER on purpose: kaniko copies as root
				// (FF_KANIKO_COPY_AS_ROOT) where docker fails, a build-outcome divergence.
				f := srcPick(s, regulars)
				var opts string
				if s.chance(2) {
					opts += fmt.Sprintf(" --chown=%s:%s", srcPick(s, uids), srcPick(s, uids))
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
				fmt.Fprintf(&b, "WORKDIR /work/dir%d\n", i)
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
				fmt.Fprintf(&b, "USER %s\n", srcPick(s, uids))
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

	return genResult{dockerfile: b.String(), context: ctx, kanikoFlags: flags}
}
