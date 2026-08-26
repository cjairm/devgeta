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

# devgeta_ob_emit_replay implements the reduction pipeline (guide §6) against
# the bytes captured so far (never including the capture notice, which is
# appended to the file only after this runs) and writes the replay to stdout.
devgeta_ob_emit_replay() {
	local capture_file="$1" head_n="$2" tail_n="$3" line_content_limit="$4"
	local max_total_bytes="$5" capture_capped="$6"

	local -a lines=()
	local had_trailing_newline=1
	if [ -s "$capture_file" ]; then
		local last_byte
		last_byte=$(tail -c1 "$capture_file")
		if [ -n "$last_byte" ]; then
			had_trailing_newline=0
		fi
	fi
	mapfile -t lines <"$capture_file"

	# Step 1: per-line truncation. A no-op for every line that already fits.
	# Deliberately not a per-line call through $(...): that forks a subshell
	# for every line, which is fine for a handful of lines and unusable for
	# the tens of thousands a real test run can produce (measured: 50k lines
	# took over two minutes). Inlined here so the common case - a line under
	# the limit - is a single array append with no subprocess at all.
	#
	# Keep the marker text below identical in shape to inlineMarkerTemplate
	# in internal/apps/baseapp/outputrules.go - that constant's length is
	# what sizes inlineMarkerReserve, and this is the text that must fit
	# inside it.
	local -a step1=()
	local i
	for ((i = 0; i < ${#lines[@]}; i++)); do
		local line="${lines[$i]}"
		local len=${#line}
		if [ "$len" -gt "$line_content_limit" ]; then
			local kept="${line:0:line_content_limit}"
			local omitted=$((len - line_content_limit))
			step1+=("${kept} [devgeta: truncated, ${omitted} bytes omitted]")
		else
			step1+=("$line")
		fi
	done

	local total_lines=${#step1[@]}

	# Step 2: head/tail selection by line count.
	local -a result=()
	local omitted_lines=0 omitted_bytes=0

	if [ "$total_lines" -gt $((head_n + tail_n)) ]; then
		omitted_lines=$((total_lines - head_n - tail_n))
		local mid_start=$head_n
		local mid_end=$((total_lines - tail_n)) # exclusive
		for ((i = mid_start; i < mid_end; i++)); do
			omitted_bytes=$((omitted_bytes + ${#lines[$i]} + 1))
		done
		for ((i = 0; i < head_n; i++)); do
			result+=("${step1[$i]}")
		done
		for ((i = mid_end; i < total_lines; i++)); do
			result+=("${step1[$i]}")
		done
	else
		result=("${step1[@]}")
	fi

	local full_path="$capture_file"
	local marker
	marker=$(devgeta_ob_marker "$omitted_lines" "$omitted_bytes" "$full_path" "$capture_capped")

	# Assemble the candidate replay: head lines, marker (if this step
	# produced one), tail lines - or, if nothing was omitted at this step,
	# every line in `result` followed by the marker only if it exists
	# (the capture-cap-only case).
	local -a assembled_lines=()
	if [ "$omitted_lines" -gt 0 ] || [ "$omitted_bytes" -gt 0 ]; then
		for ((i = 0; i < head_n && i < ${#result[@]}; i++)); do
			assembled_lines+=("${result[$i]}")
		done
		[ -n "$marker" ] && assembled_lines+=("$marker")
		for ((i = head_n; i < ${#result[@]}; i++)); do
			assembled_lines+=("${result[$i]}")
		done
	else
		assembled_lines=("${result[@]}")
		[ -n "$marker" ] && assembled_lines+=("$marker")
	fi

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
	devgeta_ob_byte_refill "$max_total_bytes" "$full_path" "$capture_capped" "${step1[@]}"
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

# devgeta_ob_byte_refill rebuilds the replay from fixed byte budgets
# (guide §6 step 3): contentBudget = maxTotalBytes - len(finalMarker) - 1,
# split 1:3 head-to-tail, taking whole lines greedily from each end.
devgeta_ob_byte_refill() {
	local max_total_bytes="$1" full_path="$2" capture_capped="$3"
	shift 3
	local -a all=("$@")
	local total=${#all[@]}

	# The marker's own text depends only on omitted counts and the path,
	# none of which changes as budgets are computed, so it can be measured
	# once up front here (unlike the head/tail step, its omitted counts are
	# not yet known - resolved with a placeholder pass below).
	local head_kept=0 head_bytes=0
	local tail_kept=0 tail_bytes=0
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
	local -a head_lines=()
	local i=0
	while [ "$i" -lt "$total" ]; do
		local l="${all[$i]}"
		local candidate_bytes=$((head_bytes + ${#l} + 1))
		if [ "$candidate_bytes" -gt "$head_budget" ]; then
			break
		fi
		head_lines+=("$l")
		head_bytes=$candidate_bytes
		head_kept=$((head_kept + 1))
		i=$((i + 1))
	done

	local -a tail_lines=()
	local j=$((total - 1))
	while [ "$j" -ge "$i" ]; do
		local l="${all[$j]}"
		local candidate_bytes=$((tail_bytes + ${#l} + 1))
		if [ "$candidate_bytes" -gt "$tail_budget" ]; then
			break
		fi
		tail_lines=("$l" "${tail_lines[@]}")
		tail_bytes=$candidate_bytes
		tail_kept=$((tail_kept + 1))
		j=$((j - 1))
	done

	local omitted_lines=$((total - head_kept - tail_kept))
	local omitted_bytes=0
	local k
	for ((k = i; k <= j; k++)); do
		omitted_bytes=$((omitted_bytes + ${#all[$k]} + 1))
	done

	local marker
	marker=$(devgeta_ob_marker "$omitted_lines" "$omitted_bytes" "$full_path" "$capture_capped")

	if [ "$omitted_lines" -le 0 ] && [ -z "$marker" ]; then
		devgeta_ob_render_lines "${all[@]}"
		printf '\n'
		return 0
	fi

	local -a assembled=("${head_lines[@]}")
	[ -n "$marker" ] && assembled+=("$marker")
	assembled+=("${tail_lines[@]}")
	devgeta_ob_render_lines "${assembled[@]}"
	printf '\n'
}

devgeta_ob_main "$@"
