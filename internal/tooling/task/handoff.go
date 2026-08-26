// The handoff task: a session's explicit, durable checkpoint for the branch
// it is working on (ADR-0032). Unlike the review journal, nothing here calls
// a model — HandoffWrite only stores whatever text it is given (ADR-0032 §4,
// "Decisions taken" #1 in the cycle doc): summarizing is a skill's job, this
// is just the storage.

package task

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/tooling/branchstore"
	"github.com/cjairm/devgeta/internal/tooling/handoff"
)

const noHandoffNoteFmt = "No handoff note for branch %s."

// handoffBranch resolves the branch a handoff note belongs to: the explicit
// branch when given, else the current one. A detached HEAD has no branch and
// therefore no note — same convention as journalBranch in reviewnotes.go.
func (tm *TaskManager) handoffBranch(branch string) (string, error) {
	if strings.TrimSpace(branch) != "" {
		return branch, nil
	}
	current, err := tm.Git.CurrentBranchIn("")
	if err != nil {
		return "", fmt.Errorf("handoff: %w", err)
	}
	if strings.TrimSpace(current) == "" {
		return "", nil
	}
	return current, nil
}

// handoffStore builds the branchstore.Store handoff notes live in, over the
// same Git app the review journal uses.
func (tm *TaskManager) handoffStore() *branchstore.Store {
	return branchstore.New(tm.Git, "handoff")
}

// HandoffWrite replaces branch's handoff note with note, stamping the current
// time and HEAD. An empty branch resolves to the current one; a detached HEAD
// has no branch to key on and is reported as the no-branch sentinel rather
// than writing a note nothing can find again.
//
// Refusing an over-cap note (handoff.ErrNoteTooLarge) never touches the
// store: Render runs before the lock is even taken, so a refused write leaves
// whatever was there before exactly as it was (ADR-0032 §2).
func (tm *TaskManager) HandoffWrite(branch, note string) (string, error) {
	target, err := tm.handoffBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return noBranchSentinel, nil
	}

	head, err := tm.Git.ResolveCommit("HEAD")
	if err != nil {
		return "", fmt.Errorf("handoff: failed to resolve HEAD: %w", err)
	}

	n := &handoff.Note{
		Branch:  target,
		Updated: tm.now().Format(time.RFC3339),
		Head:    head,
		Body:    note,
	}
	rendered, err := n.Render()
	if err != nil {
		if errors.Is(err, handoff.ErrNoteTooLarge) {
			return "", fmt.Errorf(
				"handoff note would exceed %d bytes rendered; shorten it and try again",
				handoff.MaxBytes,
			)
		}
		return "", err
	}

	store := tm.handoffStore()
	if err := store.WithLock("", func() error {
		return store.Write("", target, []byte(rendered))
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote the handoff note for %s.", target), nil
}

// HandoffRead prints branch's handoff note, or a clean "no note" sentinel
// (exit 0, not an error) when there is none — a fresh branch is the normal
// case. When the note's stamped HEAD no longer matches the current one, the
// output says so: the note may describe a tree that has since moved on.
func (tm *TaskManager) HandoffRead(branch string) (string, error) {
	target, err := tm.handoffBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return noBranchSentinel, nil
	}

	store := tm.handoffStore()
	data, err := store.Read("", target)
	if err != nil {
		return "", err
	}
	if data == nil {
		return fmt.Sprintf(noHandoffNoteFmt, target), nil
	}
	n := handoff.Parse(target, data)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Handoff note for %s (updated %s, head %s):\n\n", n.Branch, n.Updated, n.Head)
	sb.WriteString(n.Body)

	if head, herr := tm.Git.ResolveCommit("HEAD"); herr == nil && n.Head != "" && head != n.Head {
		fmt.Fprintf(
			&sb,
			"\n\n(note: HEAD has moved since this note was written — was %s, now %s)\n",
			n.Head, head,
		)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// HandoffClear deletes branch's handoff note. Clearing a note that never
// existed is success, same convention as reviewjournal.Manager.Delete.
func (tm *TaskManager) HandoffClear(branch string) (string, error) {
	target, err := tm.handoffBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return noBranchSentinel, nil
	}

	store := tm.handoffStore()
	if err := store.WithLock("", func() error {
		return store.Remove("", target)
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Cleared the handoff note for %s.", target), nil
}
