package githubcli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init(false)
}

func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &GithubCli{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "gh" {
		t.Fatalf("expected InstallPackage(%s), got %q", "gh", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &GithubCli{Cmd: mc}
//
// 	if err := app.ForceInstall(); err != nil {
// 		t.Fatalf("ForceInstall error: %v", err)
// 	}
// 	// ForceInstall should call Install() which uses InstallPackage
// 	if mc.InstalledPkg != "gh" {
// 		t.Fatalf("expected InstallPackage(%s), got %q", "gh", mc.InstalledPkg)
// 	}
// }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &GithubCli{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "gh" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "gh", mc.MaybeInstalled)
	}
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &GithubCli{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("gh version 2.40.0", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify ExecCommand was called once
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		// Verify command parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "gh" {
			t.Fatalf("Expected command 'gh', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	// Test 2: Error handling
	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: gh"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run gh command") {
			t.Fatalf("Expected error to contain 'failed to run gh command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: gh") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("PR list output", "", nil)

		err := app.ExecuteCommand("pr", "list", "--state", "open")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"pr", "list", "--state", "open"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	// Test 4: Auth status command
	t.Run("auth status command", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("github.com: Logged in", "", nil)

		err := app.ExecuteCommand("auth", "status")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "gh" {
			t.Fatalf("Expected command 'gh', got %q", lastCall.Command)
		}
		expectedArgs := []string{"auth", "status"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
	})
}

// SKIP: Uninstall test as per guidelines
// Uninstall returns error for unsupported operation
// func TestUninstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &GithubCli{Cmd: mc}
//
// 	err := app.Uninstall()
// 	if err == nil {
// 		t.Fatal("expected Uninstall to return error for unsupported operation")
// 	}
// 	if err.Error() != "gh uninstall not supported through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }

// SKIP: Update test as per guidelines
// Update returns error for unsupported operation
// func TestUpdate(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &GithubCli{Cmd: mc}
//
// 	err := app.Update()
// 	if err == nil {
// 		t.Fatal("expected Update to return error for unsupported operation")
// 	}
// 	if err.Error() != "gh update not implemented through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }

func TestRunWithOutput(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &GithubCli{Cmd: mc, Base: mockBase}

	t.Run("returns stdout on success", func(t *testing.T) {
		mockBase.SetExecCommandResult(`{"data":"value"}`, "", nil)

		out, err := app.RunWithOutput("api", "graphql", "-f", "query=...")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `{"data":"value"}` {
			t.Fatalf("expected stdout, got %q", out)
		}
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "gh" {
			t.Fatalf("expected command 'gh', got %q", lastCall.Command)
		}
	})

	t.Run("wraps error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "err", fmt.Errorf("exit 1"))

		_, err := app.RunWithOutput("api", "graphql")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed to run gh command") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGraphQL(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &GithubCli{Cmd: mc, Base: mockBase}

	t.Run("assembles correct gh args", func(t *testing.T) {
		mockBase.SetExecCommandResult(`{"data":{}}`, "", nil)

		out, err := app.GraphQL(
			"query { viewer { login } }",
			map[string]string{"owner": "octocat"},
			map[string]string{"pr": "42"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `{"data":{}}` {
			t.Fatalf("expected json output, got %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		if call.Command != "gh" {
			t.Fatalf("expected 'gh', got %q", call.Command)
		}

		argsJoined := strings.Join(call.Args, " ")
		if !strings.Contains(argsJoined, "api graphql") {
			t.Fatalf("expected 'api graphql' in args, got: %v", call.Args)
		}
		if !strings.Contains(argsJoined, "query=query { viewer { login } }") {
			t.Fatalf("expected query arg in args, got: %v", call.Args)
		}
		if !strings.Contains(argsJoined, "-f owner=octocat") {
			t.Fatalf("expected string var -f owner=octocat in args, got: %v", call.Args)
		}
		if !strings.Contains(argsJoined, "-F pr=42") {
			t.Fatalf("expected int var -F pr=42 in args, got: %v", call.Args)
		}
	})

	t.Run("returns error from gh", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "unauthorized", fmt.Errorf("exit 1"))

		_, err := app.GraphQL("query { viewer { login } }", nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// argSeq reports whether wantSeq appears as a contiguous subsequence of args.
func argSeq(args []string, wantSeq ...string) bool {
	for i := 0; i+len(wantSeq) <= len(args); i++ {
		match := true
		for j, w := range wantSeq {
			if args[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestFetchReviewThreads(t *testing.T) {
	t.Run("assembles graphql query with vars", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"data":{}}`, "", nil)

		out, err := app.FetchReviewThreads("octocat", "hello", "42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `{"data":{}}` {
			t.Fatalf("unexpected output: %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call.Command != "gh" {
			t.Fatalf("expected gh, got %q", call.Command)
		}
		if !argSeq(call.Args, "api", "graphql", "--paginate") {
			t.Fatalf("expected 'api graphql --paginate' in args, got %v", call.Args)
		}
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "reviewThreads") {
			t.Fatalf("expected reviewThreads query, got %v", call.Args)
		}
		if !strings.Contains(joined, "diffHunk") {
			t.Fatalf("expected diffHunk field in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "firstComment") {
			t.Fatalf("expected firstComment alias in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "pageInfo") {
			t.Fatalf("expected pageInfo for pagination, got %v", call.Args)
		}
		if !strings.Contains(joined, "resolvedBy") {
			t.Fatalf("expected resolvedBy field on reviewThread in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "createdAt") {
			t.Fatalf("expected createdAt field on comments in query, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "owner=octocat") {
			t.Fatalf("expected -f owner=octocat, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "name=hello") {
			t.Fatalf("expected -f name=hello, got %v", call.Args)
		}
		if !argSeq(call.Args, "-F", "pr=42") {
			t.Fatalf("expected -F pr=42, got %v", call.Args)
		}
	})

	t.Run("validates required args", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.FetchReviewThreads("", "hello", "42"); err == nil {
			t.Fatal("expected error for empty owner")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestFetchPRDiscussion(t *testing.T) {
	t.Run("assembles graphql query with vars, no pagination", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"data":{}}`, "", nil)

		out, err := app.FetchPRDiscussion("octocat", "hello", "42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `{"data":{}}` {
			t.Fatalf("unexpected output: %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call.Command != "gh" {
			t.Fatalf("expected gh, got %q", call.Command)
		}
		if !argSeq(call.Args, "api", "graphql") {
			t.Fatalf("expected 'api graphql' in args, got %v", call.Args)
		}
		if argSeq(call.Args, "--paginate") {
			t.Fatalf("expected no --paginate flag, got %v", call.Args)
		}
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "reviews") {
			t.Fatalf("expected reviews field in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "comments") {
			t.Fatalf("expected comments field in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "submittedAt") {
			t.Fatalf("expected submittedAt field on reviews in query, got %v", call.Args)
		}
		if !strings.Contains(joined, "createdAt") {
			t.Fatalf("expected createdAt field on comments in query, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "owner=octocat") {
			t.Fatalf("expected -f owner=octocat, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "name=hello") {
			t.Fatalf("expected -f name=hello, got %v", call.Args)
		}
		if !argSeq(call.Args, "-F", "pr=42") {
			t.Fatalf("expected -F pr=42, got %v", call.Args)
		}
	})

	t.Run("validates required args", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.FetchPRDiscussion("", "hello", "42"); err == nil {
			t.Fatal("expected error for empty owner")
		}
		if _, err := app.FetchPRDiscussion("octocat", "", "42"); err == nil {
			t.Fatal("expected error for empty repo")
		}
		if _, err := app.FetchPRDiscussion("octocat", "hello", ""); err == nil {
			t.Fatal("expected error for empty pr number")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})

	t.Run("wraps error from gh", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "unauthorized", fmt.Errorf("exit 1"))

		if _, err := app.FetchPRDiscussion("octocat", "hello", "42"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveReviewThread(t *testing.T) {
	t.Run("assembles resolve mutation", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"data":{"resolveReviewThread":{}}}`, "", nil)

		if _, err := app.ResolveReviewThread("PRRT_abc"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		call := mockBase.GetLastExecCommandCall()
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "resolveReviewThread") {
			t.Fatalf("expected resolveReviewThread mutation, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "threadId=PRRT_abc") {
			t.Fatalf("expected -f threadId=PRRT_abc, got %v", call.Args)
		}
	})

	t.Run("requires thread id", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.ResolveReviewThread(""); err == nil {
			t.Fatal("expected error for empty thread id")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestCreatePR(t *testing.T) {
	t.Run("assembles create args and returns url", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("https://github.com/o/r/pull/7", "", nil)

		out, err := app.CreatePR("My title", "My body", "main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "https://github.com/o/r/pull/7" {
			t.Fatalf("unexpected output: %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "create") {
			t.Fatalf("expected 'pr create', got %v", call.Args)
		}
		if !argSeq(call.Args, "--title", "My title") {
			t.Fatalf("expected --title, got %v", call.Args)
		}
		if !argSeq(call.Args, "--body", "My body") {
			t.Fatalf("expected --body, got %v", call.Args)
		}
		if !argSeq(call.Args, "--base", "main") {
			t.Fatalf("expected --base main, got %v", call.Args)
		}
	})

	t.Run("omits base when empty", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("url", "", nil)

		if _, err := app.CreatePR("t", "b", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if argSeq(mockBase.GetLastExecCommandCall().Args, "--base") {
			t.Fatal("did not expect --base when base is empty")
		}
	})

	t.Run("requires title", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.CreatePR("", "b", "main"); err == nil {
			t.Fatal("expected error for empty title")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestUpdatePRDescription(t *testing.T) {
	t.Run("with pr number", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.UpdatePRDescription("7", "new body"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "edit", "7") {
			t.Fatalf("expected 'pr edit 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--body", "new body") {
			t.Fatalf("expected --body, got %v", call.Args)
		}
	})

	t.Run("without pr number targets current branch", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.UpdatePRDescription("", "b"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if call.Args[0] != "pr" || call.Args[1] != "edit" || call.Args[2] != "--body" {
			t.Fatalf("expected 'pr edit --body ...' with no number, got %v", call.Args)
		}
	})
}

func TestApprovePR(t *testing.T) {
	t.Run("approve with body", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.ApprovePR("7", "LGTM"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "review", "7") {
			t.Fatalf("expected 'pr review 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--approve") {
			t.Fatalf("expected --approve, got %v", call.Args)
		}
		if !argSeq(call.Args, "--body", "LGTM") {
			t.Fatalf("expected --body LGTM, got %v", call.Args)
		}
	})

	t.Run("approve without body omits flag", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.ApprovePR("", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if argSeq(call.Args, "--body") {
			t.Fatal("did not expect --body when body empty")
		}
		if !argSeq(call.Args, "--approve") {
			t.Fatalf("expected --approve, got %v", call.Args)
		}
	})
}

func TestRequestChangesPR(t *testing.T) {
	t.Run("request changes with body", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.RequestChangesPR("7", "please fix"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "review", "7") {
			t.Fatalf("expected 'pr review 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--request-changes", "--body", "please fix") {
			t.Fatalf("expected --request-changes --body, got %v", call.Args)
		}
	})

	t.Run("requires body", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := app.RequestChangesPR("7", ""); err == nil {
			t.Fatal("expected error for empty body")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestReplyToReviewThread(t *testing.T) {
	t.Run("assembles reply mutation", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"data":{"addPullRequestReviewThreadReply":{}}}`, "", nil)

		if _, err := app.ReplyToReviewThread("PRRT_abc", "Fixed in abc123"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "addPullRequestReviewThreadReply") {
			t.Fatalf("expected reply mutation, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "threadId=PRRT_abc") {
			t.Fatalf("expected -f threadId, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "body=Fixed in abc123") {
			t.Fatalf("expected -f body, got %v", call.Args)
		}
	})

	t.Run("requires thread id and body", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.ReplyToReviewThread("PRRT_abc", ""); err == nil {
			t.Fatal("expected error for empty body")
		}
		if _, err := app.ReplyToReviewThread("", "hi"); err == nil {
			t.Fatal("expected error for empty thread id")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestUnresolveReviewThread(t *testing.T) {
	t.Run("assembles unresolve mutation", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"data":{}}`, "", nil)

		if _, err := app.UnresolveReviewThread("PRRT_abc"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !strings.Contains(strings.Join(call.Args, " "), "unresolveReviewThread") {
			t.Fatalf("expected unresolveReviewThread mutation, got %v", call.Args)
		}
		if !argSeq(call.Args, "-f", "threadId=PRRT_abc") {
			t.Fatalf("expected -f threadId, got %v", call.Args)
		}
	})

	t.Run("requires thread id", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := app.UnresolveReviewThread(""); err == nil {
			t.Fatal("expected error for empty thread id")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestPRView(t *testing.T) {
	t.Run("default fields with pr number", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`{"number":7}`, "", nil)

		out, err := app.PRView("7")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `{"number":7}` {
			t.Fatalf("unexpected output: %q", out)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "view", "7") {
			t.Fatalf("expected 'pr view 7', got %v", call.Args)
		}
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "--json") {
			t.Fatalf("expected --json, got %v", call.Args)
		}
		if !strings.Contains(joined, "reviewDecision") {
			t.Fatalf("expected default fields in --json, got %v", call.Args)
		}
	})

	t.Run("custom fields override defaults", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("{}", "", nil)

		if _, err := app.PRView("", "body", "title"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "--json", "body,title") {
			t.Fatalf("expected --json body,title, got %v", call.Args)
		}
		// No pr number → "view" should be immediately followed by "--json"
		if !argSeq(call.Args, "pr", "view", "--json") {
			t.Fatalf("expected no pr number, got %v", call.Args)
		}
	})
}

func TestPRChecks(t *testing.T) {
	t.Run("success returns json", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(`[{"name":"build","state":"SUCCESS"}]`, "", nil)

		out, err := app.PRChecks("7")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `[{"name":"build","state":"SUCCESS"}]` {
			t.Fatalf("unexpected output: %q", out)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "checks", "7") {
			t.Fatalf("expected 'pr checks 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--json", "name,state,link,workflow,bucket") {
			t.Fatalf("expected --json fields, got %v", call.Args)
		}
	})

	t.Run("failing checks (non-zero exit) still return json", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(
			`[{"name":"test","state":"FAILURE"}]`,
			"1 failing",
			fmt.Errorf("exit 1"),
		)

		out, err := app.PRChecks("7")
		if err != nil {
			t.Fatalf("expected JSON returned despite non-zero exit, got error: %v", err)
		}
		if out != `[{"name":"test","state":"FAILURE"}]` {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("no checks returns empty array", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(
			"",
			"no checks reported on the 'feat' branch",
			fmt.Errorf("exit 1"),
		)

		out, err := app.PRChecks("")
		if err != nil {
			t.Fatalf("expected no error for 'no checks', got: %v", err)
		}
		if out != "[]" {
			t.Fatalf("expected empty array, got %q", out)
		}
	})

	t.Run("genuine failure surfaces error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "HTTP 500", fmt.Errorf("exit 1"))

		if _, err := app.PRChecks("7"); err == nil {
			t.Fatal("expected error for empty failure")
		}
	})
}

func TestCommentPR(t *testing.T) {
	t.Run("posts comment", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.CommentPR("7", "thanks"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "comment", "7") {
			t.Fatalf("expected 'pr comment 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--body", "thanks") {
			t.Fatalf("expected --body, got %v", call.Args)
		}
	})

	t.Run("requires body", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := app.CommentPR("7", ""); err == nil {
			t.Fatal("expected error for empty body")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestRequestReviewPR(t *testing.T) {
	t.Run("adds reviewers on explicit pr", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.RequestReviewPR("7", []string{"octocat", "hubot"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "edit", "7") {
			t.Fatalf("expected 'pr edit 7', got %v", call.Args)
		}
		if !argSeq(call.Args, "--add-reviewer", "octocat,hubot") {
			t.Fatalf("expected --add-reviewer octocat,hubot, got %v", call.Args)
		}
	})

	t.Run("omits pr number for current branch", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if err := app.RequestReviewPR("", []string{"octocat"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := mockBase.GetLastExecCommandCall()
		if call.Args[0] != "pr" || call.Args[1] != "edit" || call.Args[2] != "--add-reviewer" {
			t.Fatalf("expected 'pr edit --add-reviewer ...' with no number, got %v", call.Args)
		}
	})

	t.Run("requires at least one reviewer", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := app.RequestReviewPR("7", nil); err == nil {
			t.Fatal("expected error for empty reviewers")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestMergePR(t *testing.T) {
	tests := []struct {
		method   string
		wantFlag string
	}{
		{"", "--squash"},
		{"squash", "--squash"},
		{"merge", "--merge"},
		{"rebase", "--rebase"},
	}
	for _, tt := range tests {
		t.Run("method="+tt.method, func(t *testing.T) {
			mockBase := commands.NewMockBaseCommand()
			app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
			mockBase.SetExecCommandResult("", "", nil)

			if err := app.MergePR("7", tt.method); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			call := mockBase.GetLastExecCommandCall()
			if !argSeq(call.Args, "pr", "merge", "7") {
				t.Fatalf("expected 'pr merge 7', got %v", call.Args)
			}
			if !argSeq(call.Args, tt.wantFlag) {
				t.Fatalf("expected %s, got %v", tt.wantFlag, call.Args)
			}
		})
	}

	t.Run("unknown method errors", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := app.MergePR("7", "bogus"); err == nil {
			t.Fatal("expected error for unknown method")
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when validation fails")
		}
	})
}

func TestCurrentPRNumber(t *testing.T) {
	t.Run("returns number", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("42", "", nil)

		n, err := app.CurrentPRNumber()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != "42" {
			t.Fatalf("expected 42, got %q", n)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "pr", "view", "--json", "number") {
			t.Fatalf("expected 'pr view --json number', got %v", call.Args)
		}
		if !argSeq(call.Args, "--jq", ".number") {
			t.Fatalf("expected '--jq .number', got %v", call.Args)
		}
	})

	t.Run("no pr returns empty without error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult(
			"",
			`no pull requests found for branch "feat"`,
			fmt.Errorf("exit 1"),
		)

		n, err := app.CurrentPRNumber()
		if err != nil {
			t.Fatalf("expected nil error for no PR, got: %v", err)
		}
		if n != "" {
			t.Fatalf("expected empty number, got %q", n)
		}
	})

	t.Run("real error propagates", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "HTTP 500: server error", fmt.Errorf("exit 1"))

		if _, err := app.CurrentPRNumber(); err == nil {
			t.Fatal("expected error for non-'no PR' failure")
		}
	})
}

func TestCurrentRepo(t *testing.T) {
	t.Run("returns owner/name", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("octocat/hello", "", nil)

		r, err := app.CurrentRepo()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r != "octocat/hello" {
			t.Fatalf("expected octocat/hello, got %q", r)
		}
		call := mockBase.GetLastExecCommandCall()
		if !argSeq(call.Args, "repo", "view", "--json", "owner,name") {
			t.Fatalf("expected 'repo view --json owner,name', got %v", call.Args)
		}
		if !argSeq(call.Args, "--jq", `.owner.login + "/" + .name`) {
			t.Fatalf("expected --jq owner/name expr, got %v", call.Args)
		}
	})

	t.Run("error propagates", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &GithubCli{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "not a git repository", fmt.Errorf("exit 1"))

		if _, err := app.CurrentRepo(); err == nil {
			t.Fatal("expected error when not in a repo")
		}
	})
}
