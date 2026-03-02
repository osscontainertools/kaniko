# Kaniko Test Strategy

Kaniko has three layers of testing with different purposes.

## Unit Tests

Unit tests cover individual functions across the codebase. They verify that
the current code behaves as written, but do not prove that kaniko produces
correct images. They are not the primary quality signal.

## Golden Tests

Golden tests verify the build plan kaniko computes for a given dockerfile: the
sequence of stages, steps, and dependencies it would execute, without actually
building anything. They are the correctness signal for kaniko's planning logic,
covering stage resolution, dependency ordering, skip-unused-stages, stage
squashing, and similar optimizations. Think of this as testing the compiler,
not the runtime.

When an optimization changes how kaniko plans a build, golden tests make the
change explicit and reviewable.

## Integration Tests

The integration tests are the primary quality gate. They answer the fundamental
question: does kaniko produce the same image as docker?

Tests run locally against a local registry. Each test builds the same
dockerfile with both docker and kaniko, then compares the results using
[diffoci](https://github.com/mzihlmann/diffoci). diffoci, as the name implies, diffs the layers of two oci images,
including all metadata like timestamps and image names. To pass a test the images must be identical, we added ignores for
fields where identical contents cannot be produced, ie. timestamps and logfiles.

The test matrix covers three comparisons:

- kaniko vs docker: the baseline, kaniko must produce the same filesystem as a standard docker build
- kaniko vs cached kaniko: a fully cached build must produce the same image as a cold build
- kaniko vs warmer-primed kaniko: pre-warming the base image cache must not change the output

Every bug fix must be accompanied by a new integration test Dockerfile that
reproduces the regression. This is the only way to prove the fix is correct and
to prevent the bug from coming back.
