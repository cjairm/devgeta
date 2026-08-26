#!/usr/bin/env bash
# The output-budget wrapper (docs/guides/output-budget-runner.md). Both
# configs/claude/output-budget.sh and configs/opencode/plugin/output-budget.js
# rewrite a matched command to invoke THIS script — the capping happens here,
# once, shared by both agents. Neither hook caps output itself; PreToolUse
# only sees the command before it runs and can only replace the command
# string (guide §1).
#
# Usage: output-budget-run.sh <head> <tail> <line-content-limit> \
#          <max-total-bytes> <capture-content-limit> <command>
#
# pipefail is deliberately left OFF (bash's default): PIPESTATUS[0] must be
# the wrapped command's own exit status, not the drain's (guide §4.3).
set -u

# LC_ALL=C makes every byte-length operation in this script ($#, ${#s},
# head -c) operate on bytes, not on whatever multi-byte encoding the
# invoking environment happens to use. Every budget in this contract is a
# byte count (guide §5.1); a locale-dependent character count would silently
# disagree with head -c's own byte semantics.
export LC_ALL=C

# The inline per-line truncation marker, split into its two literal halves so
# that the bash path and the awk passes further down build the identical text
# from ONE definition instead of each restating it. Keep in shape with
# inlineMarkerTemplate in internal/apps/baseapp/outputrules.go — that
# constant's length is what sizes inlineMarkerReserve, and this is the text
# that has to fit inside it.
readonly DEVGETA_OB_TRUNC_PREFIX=' [devgeta: truncated, '
readonly DEVGETA_OB_TRUNC_SUFFIX=' bytes omitted]'

devgeta_ob_run_unwrapped() {
	bash -c "$1"
	exit $?
}

devgeta_ob_main() {
	if [ "$#" -lt 6 ]; then
		# Malformed invocation - never reached by either hook's own rewrite,
		# but a hand-invocation with too few args must still not hang or crash.
		exit 1
	fi

	local head_lines="$1" tail_lines="$2" line_content_limit="$3"
	local max_total_bytes="$4" capture_content_limit="$5"
	shift 5
	local cmd="$1"

	# Escape hatch: true pass-through, no wrapping, no capture file.
	if [ "${DEVGETA_OUTPUT_BUDGET:-}" = "off" ]; then
		devgeta_ob_run_unwrapped "$cmd"
	fi

	# Numeric validation BEFORE any arithmetic (guide §5.4). A failure here
	# means run the command exactly as if this wrapper did not exist - the
	# runner is defence in depth against a hand-edited sidecar, independent
	# of the hooks' own identical check.
	local width_re='^[1-9][0-9]{0,14}$'
	local n
	for n in "$head_lines" "$tail_lines" "$line_content_limit" "$max_total_bytes" "$capture_content_limit"; do
		if ! [[ "$n" =~ $width_re ]]; then
			devgeta_ob_run_unwrapped "$cmd"
		fi
	done

	# Scratch allocation: same root and "task-" prefix `dg task scratch` uses
	# (pkg/paths.EnsureScratchDir, ScratchAllocPrefix; guide §7), so the
	# existing reaper collects this directory - never a bare file at the root.
	local scratch_root="${XDG_CACHE_HOME:-$HOME/.cache}/devgeta/scratch"
	mkdir -p "$scratch_root" 2>/dev/null
	chmod 700 "$scratch_root" 2>/dev/null
	local workdir
	workdir=$(mktemp -d "$scratch_root/task-XXXXXXXX" 2>/dev/null) || devgeta_ob_run_unwrapped "$cmd"
	local capture_file="$workdir/output"

	# Bound the capture without ever blocking or signalling the child (guide
	# §4.2): head writes up to probe_limit bytes and exits; cat keeps
	# draining the same descriptor so the producer never sees a full pipe.
	# probe_limit is one byte past the content budget, which is what lets
	# capture_capped be detected below without needing the discarded byte
	# count (guide §4.3) - captureContentLimit's width is capped at 15
	# decimal digits, so +1 here can never wrap.
	local probe_limit=$((capture_content_limit + 1))
	bash -c "$cmd" 2>&1 | {
		head -c "$probe_limit" >"$capture_file"
		cat >/dev/null
	}
	local child_status=${PIPESTATUS[0]}

	local captured_bytes
	captured_bytes=$(wc -c <"$capture_file")
	captured_bytes="${captured_bytes//[[:space:]]/}"
	local capture_capped=0
	if [ "$captured_bytes" -eq "$probe_limit" ]; then
		capture_capped=1
	fi

	devgeta_ob_emit_replay "$capture_file" "$head_lines" "$tail_lines" \
		"$line_content_limit" "$max_total_bytes" "$capture_capped"

	# The notice is appended to the FILE after the replay is built from the
	# captured bytes (guide §4.3, "Order:..."), so it can never be selected
	# into the replay's own tail.
	if [ "$capture_capped" -eq 1 ]; then
		printf '\n[devgeta: capture stopped at the limit; anything the command produced past this point, if any, is gone.]\n' >>"$capture_file"
	fi

	exit "$child_status"
}

# devgeta_ob_marker builds the one marker line used both for a head/tail (or
# byte-refill) reduction and for a capture-cap notice with no reduction at
# all - "emit the replay with the cap stated in its marker" (guide §4.3's
# ordering note). Empty omitted_lines/omitted_bytes ("0"/"0") with capped=0
# means nothing needs to be said, and this prints nothing.
devgeta_ob_marker() {
	local omitted_lines="$1" omitted_bytes="$2" full_path="$3" capped="$4"
	local body=""
	if [ "$omitted_lines" != "0" ] || [ "$omitted_bytes" != "0" ]; then
		body="${omitted_lines} lines / ${omitted_bytes} bytes omitted"
	fi
	local cap_note=""
	if [ "$capped" -eq 1 ]; then
		cap_note="output was capped at the capture limit and may be incomplete"
	fi
	if [ -z "$body" ] && [ -z "$cap_note" ]; then
		return 0
	fi
	local joined="$body"
	if [ -n "$body" ] && [ -n "$cap_note" ]; then
		joined="$body; $cap_note"
	elif [ -n "$cap_note" ]; then
		joined="$cap_note"
	fi
	printf '[devgeta: %s — full output: %s]\n' "$joined" "$full_path"
}

# devgeta_ob_truncate_line applies the per-line content limit, returning the
# result in REPLY. Deliberately not a $(...) call: command substitution forks
# a subshell per line, which is fine for a handful and unusable for the tens
# of thousands a real test run produces (measured: 50k lines took over two
# minutes that way).
devgeta_ob_truncate_line() {
	local line="$1" limit="$2"
	local len=${#line}
	if [ "$len" -gt "$limit" ]; then
		REPLY="${line:0:limit}${DEVGETA_OB_TRUNC_PREFIX}$((len - limit))${DEVGETA_OB_TRUNC_SUFFIX}"
	else
		REPLY="$line"
	fi
}

# The awk fragment that applies the same truncation as the function above,
# leaving the result in `t`. Shared verbatim by the three awk passes so the
# rendered text can never diverge between them or from the bash path.
readonly DEVGETA_OB_AWK_TRUNCATE='
	n = length($0)
	t = (n > lim) ? substr($0, 1, lim) pre (n - lim) suf : $0
'

# devgeta_ob_emit_replay implements the reduction pipeline (guide §6) against
# the bytes captured so far (never including the capture notice, which is
# appended to the file only after this runs) and writes the replay to stdout.
#
# The capture file is bounded at 16 MiB but nothing bounds its LINE count, so
# nothing here may be proportional to it. Reading the whole file into a bash
# array cost 3.8s on a 200k-line capture against 0.3s unwrapped — paid on the
# critical path of every matched command, to save tokens on output the user
# was already waiting for. Instead the reduction reads only the head_n and
# tail_n lines it can actually emit, and derives the omitted middle by
# subtracting their byte totals from the file's own size. Both reads are
# bounded by the rule's line budgets, so the cost is flat in the input size.
devgeta_ob_emit_replay() {
	local capture_file="$1" head_n="$2" tail_n="$3" line_content_limit="$4"
	local max_total_bytes="$5" capture_capped="$6"

	local total_bytes had_trailing_newline=1 total_lines=0
	total_bytes=$(wc -c <"$capture_file")
	total_bytes="${total_bytes//[[:space:]]/}"
	if [ "$total_bytes" -gt 0 ]; then
		# $(...) strips the trailing newline, so a non-empty result here means
		# the last byte was something else — i.e. no trailing newline.
		if [ -n "$(tail -c1 "$capture_file")" ]; then
			had_trailing_newline=0
		fi
		total_lines=$(wc -l <"$capture_file")
		total_lines="${total_lines//[[:space:]]/}"
		# wc -l counts newlines; a final line without one is still a line.
		if [ "$had_trailing_newline" -eq 0 ]; then
			total_lines=$((total_lines + 1))
		fi
	fi

	local -a head_src=() tail_src=()
	local omitted_lines=0 omitted_bytes=0

	if [ "$total_lines" -gt $((head_n + tail_n)) ]; then
		omitted_lines=$((total_lines - head_n - tail_n))
		mapfile -t head_src < <(head -n "$head_n" "$capture_file")
		mapfile -t tail_src < <(tail -n "$tail_n" "$capture_file")

		# Byte accounting by subtraction, on the ORIGINAL (untruncated) lines,
		# matching what the omitted-middle marker has always reported. Every
		# head line ends in a newline (none of them is the file's last line);
		# the tail's last line does only if the capture itself did.
		local l head_bytes=0 tail_bytes=0
		for l in "${head_src[@]}"; do head_bytes=$((head_bytes + ${#l} + 1)); done
		for l in "${tail_src[@]}"; do tail_bytes=$((tail_bytes + ${#l} + 1)); done
		if [ "$had_trailing_newline" -eq 0 ]; then
			tail_bytes=$((tail_bytes - 1))
		fi
		omitted_bytes=$((total_bytes - head_bytes - tail_bytes))
	else
		# At most head_n + tail_n lines, so reading them all is bounded by the
		# rule's own line budgets rather than by the size of the capture. A
		# single enormous line lands here too, and per-line truncation is what
		# bounds it.
		mapfile -t head_src <"$capture_file"
	fi

	# Step 1: per-line truncation, applied only to the lines that survive.
	local -a trunc_head=() trunc_tail=()
	local l
	for l in "${head_src[@]}"; do
		devgeta_ob_truncate_line "$l" "$line_content_limit"
		trunc_head+=("$REPLY")
	done
	for l in "${tail_src[@]}"; do
		devgeta_ob_truncate_line "$l" "$line_content_limit"
		trunc_tail+=("$REPLY")
	done

	local full_path="$capture_file"
	local marker
	marker=$(devgeta_ob_marker "$omitted_lines" "$omitted_bytes" "$full_path" "$capture_capped")

	# Assemble the candidate replay: head lines, marker (if this step produced
	# one), tail lines — or, when nothing was omitted, every line followed by
	# the marker only if one exists at all (the capture-cap-only case).
	local -a assembled_lines=("${trunc_head[@]}")
	[ -n "$marker" ] && assembled_lines+=("$marker")
	assembled_lines+=("${trunc_tail[@]}")

	# A trailing newline is added unless this is genuinely the untouched
	# passthrough case (nothing omitted, no marker at all) and the original
	# capture did not end in one either - anything devgeta itself assembled
	# (a marker present) always ends in a real newline.
	local add_trailing_newline=1
	if [ "$omitted_lines" -eq 0 ] && [ "$omitted_bytes" -eq 0 ] && [ -z "$marker" ] &&
		[ "$had_trailing_newline" -eq 0 ]; then
		add_trailing_newline=0
	fi

	local candidate
	candidate=$(devgeta_ob_render_lines "${assembled_lines[@]}")
	local candidate_bytes=${#candidate}
	if [ "$add_trailing_newline" -eq 1 ]; then
		candidate_bytes=$((candidate_bytes + 1))
	fi

	if [ "$candidate_bytes" -le "$max_total_bytes" ]; then
		printf '%s' "$candidate"
		[ "$add_trailing_newline" -eq 1 ] && printf '\n'
		return 0
	fi

	# Step 3: byte refill. Discard the line/marker-based candidate and
	# rebuild from fixed byte budgets, taking whole lines greedily.
	devgeta_ob_byte_refill "$max_total_bytes" "$full_path" "$capture_capped" \
		"$capture_file" "$line_content_limit" "$total_lines"
}

# devgeta_ob_render_lines joins an array of lines with "\n" and prints the
# result with NO trailing newline of its own — every caller decides that
# separately, printed with a plain printf rather than folded in here,
# because a caller that captures this via $(...) would otherwise always lose
# a real trailing newline to command substitution's own stripping (bash
# strips ALL trailing newlines from a captured command, independent of what
# was actually printed). That is not a hypothetical: it is the bug this
# split exists to avoid.
devgeta_ob_render_lines() {
	local out="" first=1 l
	for l in "$@"; do
		if [ "$first" -eq 1 ]; then
			out="$l"
			first=0
		else
			out="$out"$'\n'"$l"
		fi
	done
	printf '%s' "$out"
}

# devgeta_ob_middle_bytes sums the post-truncation size (plus one newline
# each) of the 1-based line range [from, to] of capture_file.
#
# Streamed through awk because the omitted middle is the one span whose length
# really is proportional to the input — it is by definition the part NOT being
# emitted, so walking it in bash would reintroduce exactly the per-line cost
# the rest of this script now avoids. awk reads it in C and stops at `to`.
devgeta_ob_middle_bytes() {
	local capture_file="$1" limit="$2" from="$3" to="$4"
	awk -v lim="$limit" -v pre="$DEVGETA_OB_TRUNC_PREFIX" -v suf="$DEVGETA_OB_TRUNC_SUFFIX" \
		-v from="$from" -v to="$to" "
		NR < from { next }
		NR > to   { exit }
		{
			$DEVGETA_OB_AWK_TRUNCATE
			total += length(t) + 1
		}
		END { printf \"%d\", total + 0 }
	" "$capture_file"
}

# devgeta_ob_take_head emits the truncated lines from line `from` onward whose
# cumulative rendered size stays within budget, stopping before the first line
# that would exceed it.
#
# The selection runs in awk rather than bash because it must be bounded by the
# BYTE budget, not by a line count. A line-count window cannot do that: the
# budget admits either many short lines or a few long ones, so any line bound
# loose enough to be correct for short lines (budget+1 of them) reads
# gigabytes when the lines are long. Selecting by bytes is bounded by
# construction — the output is at most `budget` plus one line.
devgeta_ob_take_head() {
	local capture_file="$1" limit="$2" budget="$3" from="$4"
	awk -v lim="$limit" -v pre="$DEVGETA_OB_TRUNC_PREFIX" -v suf="$DEVGETA_OB_TRUNC_SUFFIX" \
		-v budget="$budget" -v from="$from" "
		NR < from { next }
		{
			$DEVGETA_OB_AWK_TRUNCATE
			b = length(t) + 1
			if (used + b > budget) exit
			used += b
			print t
		}
	" "$capture_file"
}

# devgeta_ob_take_tail emits the longest run of truncated lines ending at the
# last line (and starting no earlier than `from`) whose cumulative rendered
# size stays within budget — the same set the equivalent backwards greedy walk
# selects, since adding lines from the end only ever increases the total.
#
# Held as a sliding window so memory stays bounded by the budget plus one line
# no matter how many lines the capture has. A single line larger than the
# whole budget evicts itself and yields nothing, which is the documented
# outcome (guide §6 step 3), not an error.
devgeta_ob_take_tail() {
	local capture_file="$1" limit="$2" budget="$3" from="$4"
	awk -v lim="$limit" -v pre="$DEVGETA_OB_TRUNC_PREFIX" -v suf="$DEVGETA_OB_TRUNC_SUFFIX" \
		-v budget="$budget" -v from="$from" "
		BEGIN { first = 1; last = 0; used = 0 }
		NR < from { next }
		{
			$DEVGETA_OB_AWK_TRUNCATE
			last++
			buf[last] = t
			size[last] = length(t) + 1
			used += size[last]
			while (used > budget && first <= last) {
				used -= size[first]
				delete buf[first]
				delete size[first]
				first++
			}
		}
		END { for (i = first; i <= last; i++) print buf[i] }
	" "$capture_file"
}

# devgeta_ob_byte_refill rebuilds the replay from fixed byte budgets
# (guide §6 step 3): contentBudget = maxTotalBytes - len(finalMarker) - 1,
# split 1:3 head-to-tail, taking whole lines greedily from each end.
devgeta_ob_byte_refill() {
	local max_total_bytes="$1" full_path="$2" capture_capped="$3"
	local capture_file="$4" line_content_limit="$5" total="$6"

	# The marker's own text depends only on omitted counts and the path,
	# none of which changes as budgets are computed, so it can be measured
	# once up front here (unlike the head/tail step, its omitted counts are
	# not yet known - resolved with a placeholder pass below).
	local head_kept=0 tail_kept=0
	local content_budget

	# Two-pass: first with a marker sized for "all bytes omitted" (the
	# longest this marker can get for this input), to guarantee the reserved
	# budget never comes up short once real counts are known - the same
	# over-reserve-on-purpose approach the inline marker uses.
	local probe_marker
	probe_marker=$(devgeta_ob_marker "$total" "999999999999999" "$full_path" "$capture_capped")
	content_budget=$((max_total_bytes - ${#probe_marker} - 1))
	if [ "$content_budget" -lt 0 ]; then
		content_budget=0
	fi
	local head_budget=$((content_budget / 4))
	local tail_budget=$((content_budget - head_budget))

	# Stop BEFORE any line — including the very first — that would exceed
	# its budget (guide §6 step 3: "stopping before the line that would
	# exceed each budget"). Ending up with zero head or tail lines when even
	# the first candidate is too big is a legitimate outcome, not a bug: the
	# alternative (always keep at least one) is exactly what let a single
	# huge line blow through the budget it was supposed to be bounded by.
	# Both selections are byte-bounded in awk, so what comes back into bash is
	# never larger than the budget plus one line — independent of how many
	# lines the capture holds or how long any of them is.
	local -a head_lines=() tail_lines=()
	mapfile -t head_lines < <(
		devgeta_ob_take_head "$capture_file" "$line_content_limit" "$head_budget" 1
	)
	head_kept=${#head_lines[@]}

	# The tail starts after whatever the head already claimed, which is what
	# keeps the two from overlapping on a short capture.
	mapfile -t tail_lines < <(
		devgeta_ob_take_tail "$capture_file" "$line_content_limit" "$tail_budget" \
			$((head_kept + 1))
	)
	tail_kept=${#tail_lines[@]}

	local omitted_lines=$((total - head_kept - tail_kept))
	local omitted_bytes=0
	if [ "$omitted_lines" -gt 0 ]; then
		omitted_bytes=$(devgeta_ob_middle_bytes "$capture_file" "$line_content_limit" \
			$((head_kept + 1)) $((total - tail_kept)))
	fi

	local marker
	marker=$(devgeta_ob_marker "$omitted_lines" "$omitted_bytes" "$full_path" "$capture_capped")

	local -a assembled=("${head_lines[@]}")
	[ -n "$marker" ] && assembled+=("$marker")
	assembled+=("${tail_lines[@]}")
	devgeta_ob_render_lines "${assembled[@]}"
	printf '\n'
}

devgeta_ob_main "$@"
