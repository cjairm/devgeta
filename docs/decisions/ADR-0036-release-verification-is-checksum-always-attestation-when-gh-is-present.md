# ADR-0036 — Release verification is checksum-always, attestation-when-`gh`-is-present

**Date:** 2026-08-25
**Status:** ACCEPTED

Part of the cycle
[2026-08-24 — performance and resource hardening](../plans/cycles/2026-08-24-performance-and-resource-hardening.md),
Step 4 ("Release artifact verification — P0-3").

## Context

`install.sh:158` fetches the release binary, `:166` checks only that it is
non-empty, `:171` `chmod +x`'s it, `:173` **executes it**
(`"$TMP_BINARY" --version`), and `:180` moves it onto `PATH`. No checksum, no
signature, anywhere in the script. This is the highest blast radius in the
repo — it is how every user gets devgeta — and it contradicts CLAUDE.md §4,
"Never execute arbitrary downloaded code without verification."

It could not verify anything even if it wanted to.
`.github/workflows/release.yml` (pre-fix) listed only the four platform
binaries in the `softprops/action-gh-release@v1` `files:` block — no
`checksums.txt` asset was produced — and all three actions it used were
pinned to mutable tags, so the build itself had no supply-chain pin.

**What a checksum can and cannot do here.** `install.sh:134` asks the API for
`releases/latest` and `:153` builds the download URL from whatever tag came
back; there is no pinned version anywhere in the script. A `checksums.txt`
published as an asset of that same release is fetched over the same
channel, from the same mutable release, under the same credentials — so an
attacker who can replace `devgeta-darwin-arm64` can replace `checksums.txt`
in the same motion, and the check passes. It buys real protection against a
truncated or corrupted transfer and against a network attacker who cannot
also write to the release; it buys **nothing** against a compromised token,
workflow, or account. Calling that "verification" of downloaded code would
satisfy the letter of CLAUDE.md §4 while leaving the threat it names
untouched — so this decision does not stop at a checksum.

A verification scheme that goes further needs a trust root outside the
release assets. The one that fits a zero-dependency installer (product
principle 1, "no pre-installed tools required beyond bash/curl") is GitHub's
build-provenance attestation: `actions/attest-build-provenance` signs a
statement binding each binary's digest to the workflow run that produced it
and records it in Sigstore's public transparency log. That signature chains
to the transparency log and the workflow's own identity, not to a release
asset an attacker who compromised the pipeline could also rewrite, and it
needs no maintainer-held key.

## Decision

**Two-tier verification, in this order, both fail closed:**

1. **SHA-256 against `checksums.txt` — always.** This is the corruption
   check: the only one available with nothing beyond bash and curl, so it
   runs unconditionally and is the floor every install gets.
2. **`gh attestation verify "$TMP_BINARY" --repo "$REPO"` — additionally,
   whenever `gh` is on `PATH`.** This is the authenticity check: it chains to
   Sigstore's transparency log rather than to anything published alongside
   the binary it is checking.
3. **`install.sh` prints which check(s) actually ran.** A script that reports
   "verified" without saying what it verified repeats, inside the product,
   the exact overclaim this ADR just spent two paragraphs rejecting for a
   bare checksum.
4. **Never a soft "skip if missing."** Treating an absent `checksums.txt` or
   a missing `gh` as "nothing to check, proceed" turns the corresponding
   check into a downgrade an attacker can trigger simply by withholding the
   file — so both checks that do run must fail closed, and the checksum
   check in particular must never be skipped just because the asset is
   absent.

**Prerequisite this task ships (the workflow half, real code, not scoped —
see the follow-up cycle doc below for the `install.sh` half):**
`.github/workflows/release.yml` now emits `sha256sum devgeta-* >
checksums.txt` and lists it in the release's `files:` block; runs
`actions/attest-build-provenance` over the four binaries (needs
`id-token: write` and `attestations: write`, added to the workflow's
`permissions:`); and pins `actions/checkout`, `actions/setup-go`,
`softprops/action-gh-release`, and `actions/attest-build-provenance` to
commit SHAs instead of mutable tags. None of this is optional infrastructure
for the policy above — without `checksums.txt` on the release page, tier 1
has nothing to check against; without the attestation, tier 2 has nothing to
verify.

## Consequences

**What this buys:** integrity against transfer corruption and against a
network attacker who can intercept or tamper with the download but cannot
also write to the GitHub release, for every install (tier 1, unconditional).
For any install where `gh` happens to be present, it additionally buys
authenticity that chains to a trust root outside the release assets — a
forged binary-plus-checksum pair produced by anyone other than this
repository's own release workflow fails the attestation check even if it
passes the SHA-256 check.

**What it does not buy — stated plainly, not softened:** a compromised
release token, workflow, or maintainer account can rewrite a binary and
`checksums.txt` in the same motion, and tier 1 alone will not catch that —
the checksum's trust root is the same release it is checking. Tier 2 closes
that gap, but only for the subset of users who have `gh` installed;
bash+curl alone cannot verify a Sigstore attestation, so a user without `gh`
is left with integrity only, never authenticity, no matter what this ADR
decides. That gap is real and is not this ADR's to close — see the open
question below.

**What becomes harder:** the release workflow now depends on three pinned
commit SHAs (plus a fourth for the new attestation action) instead of three
mutable tags, so a future Dependabot/renovate-style bump has to move a SHA
and its trailing version comment together, not just edit a version number.

**Explicitly NOT decided by this ADR:** whether authenticity should ever
become _mandatory_ — i.e., `install.sh` requiring `gh` to be present, or
shipping an embedded public key with a `minisign`/`cosign` signature check
instead of (or alongside) the attestation. Both routes break product
principle 1 unless something changes, or add key-management burden of their
own — and `install.sh` is itself served unpinned from
`raw.githubusercontent.com/.../main/install.sh`, so an embedded key would
only ever be as good as that same fetch. This is left as an open question
for a future ADR; the two-tier policy above is the default that applies
until that future ADR says otherwise.
