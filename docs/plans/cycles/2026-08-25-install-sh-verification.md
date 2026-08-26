# Cycle: `install.sh` verifies what it downloads

**Date:** 2026-08-25
**Estimated Duration:** ~2 hours
**Status:** Done

---

**Pre-flight gate passed on 2026-08-26 against release `v1.22.0`.** That
release carries `checksums.txt` alongside the four binaries;
`gh attestation verify` passes against a published binary (SLSA provenance v1,
`.github/workflows/release.yml @ refs/tags/v1.22.0`); all four attested digests
match `checksums.txt`; and a one-byte-tampered copy fails closed with exit 1.
The blocker this section carried — that shipping the verification before a
release actually carried the assets would hard-fail every `curl … | bash`
install until the next tag — no longer applies. The rollout-order reasoning is
kept below for the record.

---

## 1. Domain Context

`install.sh` is devgeta's zero-dependency installer: `curl -fsSL
.../install.sh | bash`. It resolves `releases/latest` from the GitHub API,
downloads the platform binary for that release, and installs it onto
`PATH`. Today it executes that binary (`"$TMP_BINARY" --version`) with no
verification at all — see [ADR-0036](../../decisions/ADR-0036-release-verification-is-checksum-always-attestation-when-gh-is-present.md)
for the full defect writeup and the verification policy this cycle
implements. That ADR is already decided; this cycle does not reopen it, it
only scopes the code change.

The workflow half of the fix — `checksums.txt` as a release asset,
`actions/attest-build-provenance` over the four binaries, and the three
pre-existing actions pinned to commit SHAs — already shipped (the task that
produced this doc). This cycle is the second half: making `install.sh`
actually use those artifacts before it runs anything it downloaded.

## 2. Engineer Context

- **Relevant files:**
  - `install.sh` — the script this cycle changes. The line numbers cited in
    this doc were re-read at implementation time (2026-08-26) and had **not**
    drifted from the workflow-half task: `:134` resolved `releases/latest`,
    `:153` built the download URL, `:158` downloaded, `:166` checked only
    non-emptiness, `:171` `chmod +x`, `:173` executed it
    (`"$TMP_BINARY" --version`), `:180` moved it onto `PATH`.

    **After this cycle's change** those are: `:152` resolves `releases/latest`,
    `:171` builds the download URL, `:176` downloads, `:184` checks
    non-emptiness, `:186`–`:277` is the new verification block (`:214`
    fetches `checksums.txt`, `:259` runs `gh attestation verify`), `:279`
    `chmod +x`, `:281` executes it, `:288` moves it onto `PATH`.

  - `.github/workflows/release.yml` — already emits `checksums.txt` and the
    attestation; this cycle's script downloads and checks against them, it
    does not touch the workflow again.
  - `docs/guides/releasing.md` — gets a new section documenting the residual
    gap (see Scope below).
  - [ADR-0036](../../decisions/ADR-0036-release-verification-is-checksum-always-attestation-when-gh-is-present.md) —
    the design decision this cycle implements. Reference it from the code
    comment at the verification block, not just from this doc.

- **External command this introduces:** `gh attestation verify
"$TMP_BINARY" --repo "$REPO"` — only when `gh` is already on `PATH`.
  Nothing in this cycle may make `gh` a hard requirement; that would break
  product principle 1 ("no pre-installed tools required beyond bash/curl"),
  which is exactly what ADR-0036 preserves by keeping the SHA-256 check
  unconditional.

- **Testing pattern for shell scripts in this repo:** the root package
  (`go test .`) covers the embedded-config tests and the reviewer/guard
  shell scripts (`agent-config-guard`, `secret-guard`, `suppression-guard`,
  `task-redirect`, `agent-state`) — see CLAUDE.md §6. `install.sh` itself is
  not embedded and not covered there; check whether it has any existing
  test harness before assuming one needs to be built from scratch.

## 3. Objective

`install.sh` never executes a downloaded binary without first checking its
SHA-256 against the release's `checksums.txt`, and additionally verifying
its build provenance via `gh attestation verify` whenever `gh` is present —
failing closed on either mismatch, and printing which check(s) actually ran.

## 4. Scope Boundary

### In Scope

- [x] SHA-256 verification against the release's `checksums.txt`, run before
      `chmod +x` (currently `install.sh:171`) — the corruption check, works
      with bash+curl alone, runs unconditionally.
- [x] When `gh` is present on `PATH`: `gh attestation verify "$TMP_BINARY"
--repo "$REPO"`, failing closed on a mismatch — the authenticity
      check.
- [x] The script prints which of the two checks actually ran, every time —
      never a bare "verified".
- [x] Both checks fail closed. In particular: a missing `checksums.txt`
      is a hard failure, not a skip — a soft "skip if missing" is a
      downgrade an attacker can trigger simply by withholding the file.
      Only the _presence_ of `gh` is conditional; once a check runs (or is
      supposed to run because its prerequisite asset should exist), it must
      pass or the install stops.
- [x] Document the residual gap in `docs/guides/releasing.md`: with only
      bash and curl, the floor this buys is integrity, not authenticity —
      point readers at ADR-0036 for the full reasoning rather than
      re-explaining it there.
- [x] Reference ADR-0036 in the code comment at the verification block.

### Explicitly Out of Scope

- Making authenticity (the attestation check) mandatory, or requiring `gh`,
  or adding an embedded public key / `minisign`/`cosign` signature check.
  ADR-0036 explicitly leaves that decision open for a future ADR; this cycle
  implements the default that applies until that ADR exists, not a
  replacement for it.
- Any further change to `.github/workflows/release.yml`. That shipped in
  the prerequisite task; if this cycle discovers the workflow output
  doesn't match what `install.sh` needs, that is a bug in the prerequisite
  to fix separately, not scope creep into this cycle.
- Pinning `install.sh` itself to a version, or changing how it resolves
  `releases/latest`. Out of scope per the plan text this cycle implements —
  raised only as context for why an embedded key wouldn't fully close the
  gap, not as something to fix here.

**Scope is locked.** If something in scope turns out to need the
out-of-scope items above, stop and escalate rather than expanding this
cycle's boundary.

## 5. Pre-flight Gate (must pass before Step 1)

1. Confirm the prerequisite workflow task has merged to `main`.
2. Trigger or wait for a real tagged release (any tag matching `v*`).
3. Open that release's actual GitHub page and confirm by eye:
   - `checksums.txt` is listed as an asset alongside the four binaries.
   - The release (or its run in the Actions tab) shows a build-provenance
     attestation was generated — `gh attestation verify` against one of the
     published binaries locally is the most direct check.
4. Only once both are confirmed, flip this doc's Status to "In Progress" and
   begin Step 1.

Skipping this gate reproduces the exact hazard ADR-0036's prerequisite task
avoided by not shipping this half early: `install.sh` is fetched from `main`
on every install, so if this cycle's code lands before a release actually
carries these assets, `curl … | bash` breaks for everyone until the next
tag.

## 6. Implementation Plan (sketch — refine once the gate above passes)

### File Changes

| Action | File Path                  | Description                                                                                                               |
| ------ | -------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Modify | `install.sh`               | Add checksum download + verification before `chmod +x`; add conditional `gh attestation verify`; print which check(s) ran |
| Modify | `docs/guides/releasing.md` | Document the integrity-vs-authenticity gap, referencing ADR-0036                                                          |

### Step-by-Step (sketch)

1. After downloading `$TMP_BINARY`, also download `checksums.txt` from the
   same release. Fail the install (not a warning) if that download fails —
   a missing checksums file is not "nothing to check."
2. Compute the SHA-256 of `$TMP_BINARY` and compare it against the matching
   line in `checksums.txt` for this platform's binary name. Fail closed on
   any mismatch, before `chmod +x`.
3. Detect `gh` on `PATH`. If present, run `gh attestation verify
"$TMP_BINARY" --repo "$REPO"`; fail closed on a non-zero exit.
4. Print a one-line summary of exactly which check(s) ran and passed (e.g.
   "checksum verified" vs. "checksum + attestation verified") before
   proceeding to `chmod +x` and the move onto `PATH`.
5. Update `docs/guides/releasing.md` with the residual-gap note and a link
   to ADR-0036.

Refine this into 5–15 minute steps with explicit `Verify:` commands once the
pre-flight gate has passed and the current `install.sh` line numbers are
re-read — this sketch is deliberately not final so the doc doesn't encode
stale line numbers as fact.

## 7. Verification Plan (sketch)

- Manual: run the modified `install.sh` against a real tagged release that
  has passed the pre-flight gate, on at least macOS; confirm it verifies
  and installs normally.
- Manual: deliberately corrupt a locally-served `checksums.txt` (or point
  the script at a mismatched binary) and confirm the script fails closed
  rather than installing.
- Manual: run once with `gh` on `PATH` and once with it removed from `PATH`
  for the test, confirming the printed summary names the right check(s) in
  each case.
- No `go test` coverage is expected to exist for `install.sh` unless one is
  discovered during Step 1 — this is a plain POSIX shell script, not Go.

### Results (2026-08-26, macOS, against `v1.22.0`)

Run in a throwaway `HOME`, with a stub `curl` on `PATH` for the cases that
need a controlled release. All eight cases behaved as designed; the three
fail-closed cases left no binary installed and no temp file behind.

| Case                                                     | Result                                                                                     |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Real release, `gh` authenticated                         | exit 0 — `Verified: SHA-256 checksum (integrity) + GitHub build provenance (authenticity)` |
| Real release, `gh` installed but not authenticated       | exit 0 — checksum only, plus an explicit `Not verified` line                               |
| Real release, `gh` absent from `PATH`                    | exit 0 — checksum only, `Not verified: … gh is not installed`                              |
| Binary tampered, `checksums.txt` genuine                 | **exit 1** — SHA-256 mismatch, expected/actual both printed                                |
| `checksums.txt` absent from the release                  | **exit 1** — hard failure, not a skip                                                      |
| `checksums.txt` present, no line for this platform       | **exit 1** — hard failure                                                                  |
| Forged binary **with** a matching forged `checksums.txt` | **exit 1** — tier 1 passes, tier 2 catches it (this is the case tier 2 exists for)         |
| `--local /path/to/binary`                                | exit 0 — `Local install: no release verification performed`                                |

No `go test` harness for `install.sh` was found, and no Go code in the repo
references it (the `install.sh` hits under `internal/` are Homebrew's and
Claude's own installers). Nothing in the Go tree changed, so no package tests
apply to this cycle.

## 8. Risks & Trade-offs

| Risk                                                               | Likelihood | Mitigation                                                                  |
| ------------------------------------------------------------------ | ---------- | --------------------------------------------------------------------------- |
| Starting before the pre-flight gate passes breaks live installs    | Med        | Gate is a hard blocker in this doc's header; check the release page by hand |
| Platform binary name in `checksums.txt` doesn't match local naming | Low        | Verify against a real `checksums.txt` from the prerequisite task's format   |
| `gh attestation verify` output/exit code changes upstream          | Low        | Pin behavior to what's observed at implementation time; re-check before use |

### Trade-offs Made

- **Fail closed on missing `checksums.txt` vs. warn-and-continue:** always
  fail closed. A warn-and-continue path is exactly the downgrade an attacker
  can trigger by withholding the file — ADR-0036 rules this out explicitly.
- **Requiring `gh` vs. optional:** optional, per ADR-0036's still-open
  question on making authenticity mandatory. This cycle implements the
  default, not a resolution of that question.

## 9. Cross-Model Review Notes

- [ ] Pre-flight gate is unambiguous and someone unfamiliar with this cycle
      could execute it themselves before writing any code?
- [ ] Scope boundary correctly excludes making `gh` mandatory or adding a
      signature scheme — that's still an open ADR question, not this
      cycle's to decide?
- [ ] Does the plan fail closed everywhere a check could be skipped?

**Reviewer notes:**

**One implementation call ADR-0036 does not literally cover — needs a human
decision.** ADR-0036 makes tier 2 conditional on `gh` being _present_. It does
not say what to do when `gh` is present but **not authenticated**, which was
measured at implementation time to be a distinct state: `gh attestation verify`
then exits **4** with `To get started with GitHub CLI, please run: gh auth
login`, and it reports that identically whether or not the binary is genuine
(exit 0 on a good binary, exit 1 on a forged one, exit 4 when it cannot ask).

The script treats that as "tier 2 could not run" — the same state as `gh` being
absent — checked up front via `gh auth status` rather than by reading exit codes
back out of `verify`, so any non-zero from `verify` itself is unambiguously a
failed verification. It is printed every time, never counted as a pass.

The argument for it: an unauthenticated `gh` is common (installed via Homebrew,
never logged in), and failing closed there would break installs for those users
without telling them anything about the binary's authenticity — the tool simply
could not look. The argument against it: it is a second conditional path, and
ADR-0036 §4 says never treat a missing input as "nothing to check."

If the stricter reading is wanted instead — an unauthenticated `gh` is a hard
failure — it is a three-line change to the `elif` chain in `install.sh`. Flagged
rather than decided alone, per this doc's "Ask before relitigating ADR-0036."

**Out-of-scope defect found, not fixed:** the comment above the "Generate
checksums" step in `.github/workflows/release.yml` cites **ADR-0031**; the ADR
it means is **ADR-0036**. §4 puts any further change to that workflow out of
scope, so it is left for a separate fix.

---

## Reference: the plan text this cycle implements

Quoted verbatim from the performance-and-resource-hardening cycle's Step 4,
for context if this doc is read on its own:

> 2. `install.sh`: verify the SHA-256 against `checksums.txt` before
>    `chmod +x` (`:171`) — that is the corruption check, and it is the only
>    one available with bash and curl alone. Then, **when `gh` is present**,
>    run `gh attestation verify "$TMP_BINARY" --repo "$REPO"` and fail closed
>    on a mismatch. Print which of the two checks actually ran; a script
>    that says "verified" without saying what it verified is the overclaim
>    moved from the plan into the product.
> 3. Document the residual gap in the release guide: with only bash and
>    curl, the floor is integrity, not authenticity.
>
> **Rollout order — the two changes cannot ship together.** `install.sh` is
> fetched live from `main` (see its own usage line at `install.sh:10`),
> while the assets it verifies only exist from the next tag onward. Merging
> the verification and the workflow change in one release means every
> `curl … | bash` between that merge and the next tag fetches a script that
> demands a `checksums.txt` the current `latest` release does not have — a
> hard failure for every new install in that window, and for anyone
> re-running the installer. Because the script always resolves `latest`
> (`:134`, `:153`), older releases are not otherwise reachable, so the
> exposure is that window rather than the whole release history. Sequence
> it:
>
> - **Release N** — workflow change only: `checksums.txt`, attestation, SHA
>   pins. `install.sh` untouched.
> - **Release N+1** — `install.sh` starts verifying, after confirming
>   release N's page actually carries the asset.
>
> Never make the verification a soft "skip if missing": that is a downgrade
> an attacker can trigger by withholding the file.

Full source:
[2026-08-24-performance-and-resource-hardening.md](2026-08-24-performance-and-resource-hardening.md),
Step 4.

## Notes for Implementers

- **This doc is a scope, not a green light.** Status stays "Draft — NOT
  approved for implementation" until a human has completed the pre-flight
  gate and explicitly approved starting.
- **Re-read `install.sh` before writing a single line.** Every line number
  cited here will be stale by the time this cycle starts; the prerequisite
  task and anything else that lands on `main` in between will have shifted
  them.
- **Ask before relitigating ADR-0036.** Its policy (checksum always,
  attestation when `gh` is present, fail closed, print which check ran) is
  a decision already made — this cycle implements it, it doesn't second-guess it.
