# ADR-0041 — An archive skips only what is provably regenerable

**Date:** 2026-09-13
**Status:** ACCEPTED

## Context

Archiving a developer's documents folder is slow mostly because of folders that
hold hundreds of thousands of small files the developer never wrote and can
rebuild: `node_modules`, virtualenvs, Rust `target/`, tool caches. Skipping them
turns a run of hours into minutes.

But the command's promise is that **nothing the user or another developer wrote
is lost**. A skipped file that could not be rebuilt is a silent data loss found
months later, on a machine that no longer has the original. Three ways to decide
what to skip were considered:

1. **By folder name** (`node_modules`, `build`, `dist`, `vendor`, …). Names lie:
   Go projects commit `vendor/`; `build/` and `dist/` are sometimes hand-written;
   any folder can be called `env`.
2. **By `.gitignore`** — git already knows what is ignored. But `.env` files,
   local config, notes and scratch data are ignored precisely because they are
   personal and not in the remote. Those are the files a migration most needs.
3. **By proof.** Skip a folder only when something next to or inside it proves a
   tool generated it and can generate it again.

## Decision

**Proof only.** A directory or file is skipped only when a rule below matches;
anything a rule does not match is archived. When in doubt, keep it — an archive
that is somewhat larger is recoverable, a missing file is not.

Proofs, in order of strength:

- **`CACHEDIR.TAG`** ([spec](https://bford.info/cachedir/)) — a regular file whose
  first 43 bytes are exactly `Signature: 8a477f597d28d172789f06886806bc55`. The
  standard marker honored by GNU tar, restic and borg, and written by Cargo,
  mypy, pytest, ruff and others. The signature is checked, not just the name, as
  the spec asks.
- **Contents** — a directory containing `pyvenv.cfg` is a Python virtualenv
  whatever it is called. A `._*` file is skipped only if it starts with the
  AppleDouble magic `0x00051607`.
- **A manifest next to it** — the tool that rebuilds the folder, identified by the
  file that drives it:

  | Skipped folder                                                             | Only when the parent contains                                                  |
  | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
  | `node_modules`, `.next`, `.nuxt`, `.svelte-kit`, `.turbo`, `.parcel-cache` | `package.json`                                                                 |
  | `target`                                                                   | `Cargo.toml`                                                                   |
  | `vendor`                                                                   | `composer.json`, and `vendor/modules.txt` is **absent** (Go)                   |
  | `Pods`                                                                     | `Podfile`                                                                      |
  | `.gradle`                                                                  | `settings.gradle`, `settings.gradle.kts`, `build.gradle` or `build.gradle.kts` |
  | `.dart_tool`                                                               | `pubspec.yaml`                                                                 |
  | `_build`, `deps`                                                           | `mix.exs`                                                                      |
  | `.zig-cache`, `zig-cache`                                                  | `build.zig`                                                                    |
  | `.tox`                                                                     | `tox.ini`, `setup.cfg` or `pyproject.toml`                                     |
  | `.terraform`                                                               | any `*.tf` file                                                                |

- **Unambiguous names** — names no tool or person uses for anything but a cache:
  `__pycache__`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, and the Finder
  file `.DS_Store`.

Never skipped, by explicit decision: `.git` (unpushed branches, stashes, local
config), `build`, `dist`, `out`, `coverage`, `bin`, and anything matched only by
`.gitignore`.

Every skip is visible: `--dry-run` lists each skipped path, the rule that matched,
and its size, and a real run writes the same list next to the archive.
`--no-skip` disables every rule. The rule table lives in one Go file with a test
per rule, including the negative case that proves the rule does **not** fire
without its proof.

## Consequences

- **Easier:** users can trust the defaults without reading the table; the
  dry-run makes each decision checkable before a long run.
- **Harder:** the list grows only by adding a proof, not a name — a new
  ecosystem needs someone to identify its manifest file.
- **Accepted:** some junk is kept — `build/` from a Gradle project, `dist/` from a
  bundler, `coverage/`. Those folders are rarely the huge-file-count ones, and
  the cost of keeping them is size, not correctness.
