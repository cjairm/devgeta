package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/pkg/paths"
)

// representativeGlobalConfigYAML is a stand-in for a real
// ~/.config/devgeta/global_config.yaml after a full `dg install`: every
// section GlobalConfig actually has (already_installed, installed across
// all seven categories, shortcuts, shell, failed_installations, worktree
// with recent_repos/search_paths, integrations, review) populated with
// plausible content. It is sized to sit in the same neighbourhood as the
// 4,313-byte file ADR-0030 was itself benchmarked against, so the number
// this benchmark produces is comparable to that ADR's indicative read/parse
// split rather than measuring some other-shaped document.
const representativeGlobalConfigYAML = `app_path: /Users/dev/.devgeta
config_path: /Users/dev/.config/devgeta
already_installed:
  packages:
    - git
    - curl
    - jq
    - autoconf
    - bison
  desktop_apps:
    - brave
  fonts: []
  themes: []
  terminal_tools:
    - zsh
  dev_languages: []
  databases: []
current_font: JetBrainsMono Nerd Font
current_theme: gruvbox-dark
installed:
  packages:
    - autoconf
    - bison
    - fontconfig
    - fastfetch
    - btop
  desktop_apps:
    - alacritty
    - raycast
    - docker
    - flameshot
    - rectangle
  fonts:
    - JetBrainsMono Nerd Font
    - FiraCode Nerd Font
  themes:
    - gruvbox-dark
    - gruvbox-light
  terminal_tools:
    - tmux
    - neovim
    - fzf
    - bat
    - eza
    - zoxide
    - lazygit
    - lazydocker
    - starship
    - htop
    - ncdu
    - tldr
    - ripgrep
    - fd
    - jq
    - zsh-autosuggestions
    - zsh-syntax-highlighting
    - powerlevel10k
  dev_languages:
    - node
    - python
    - go
    - rust
    - php
  databases:
    - postgresql
    - redis
    - mysql
    - mongodb
    - sqlite
shortcuts:
  tmux-attach: "tmux attach -t main"
  worktree-new: "dg ws new"
  config-edit: "dg config edit"
  review-run: "dg task review-run"
  worktree-attach: "dg ws attach"
  worktree-list: "dg ws list"
  worktree-remove: "dg ws remove"
  install-all: "dg install"
  install-terminal: "dg install --only terminal"
  install-languages: "dg install --only languages"
  install-databases: "dg install --only databases"
  install-desktop: "dg install --only desktop"
  config-get: "dg config get"
  config-set: "dg config set"
  status-check: "dg status"
  logs-tail: "dg logs --tail"
  update-check: "dg update --check"
  theme-switch: "dg theme set gruvbox-dark"
  font-switch: "dg font set JetBrainsMono Nerd Font"
  bench-run: "dg task bench-run"
  release-cut: "devgeta task release"
  ws-dashboard: "dg ws"
  gain-report: "rtk gain"
shell:
  is_mac: true
  mise: true
  zoxide: true
  zsh_autosuggestions: true
  zsh_syntax_highlighting: true
  powerlevel10k: true
  extended_capabilities: true
  lazy_git: true
  lazy_docker: true
  fzf: true
  neovim: true
  tmux: true
  eza: true
  bat: true
  opencode: true
  claude: true
failed_installations:
  - package_name: mongodb
    category: database
    error_message: "brew install mongodb-community failed: formula not found"
    failed_at: 2026-08-20T14:32:07Z
    attempt_count: 2
  - package_name: rust
    category: dev_language
    error_message: "mise install rust@stable timed out after 300s"
    failed_at: 2026-08-21T09:11:45Z
    attempt_count: 1
  - package_name: sqlite
    category: database
    error_message: "brew install sqlite failed: could not link, target already exists"
    failed_at: 2026-08-19T16:20:03Z
    attempt_count: 1
  - package_name: php
    category: dev_language
    error_message: "mise install php@8.3 failed: build dependency openssl@3 missing"
    failed_at: 2026-08-18T10:05:22Z
    attempt_count: 3
  - package_name: docker
    category: package
    error_message: "brew install --cask docker failed: Docker.app already exists at destination"
    failed_at: 2026-08-17T07:40:59Z
    attempt_count: 1
  - package_name: redis
    category: database
    error_message: "brew services start redis failed: launchctl bootstrap exited with 5"
    failed_at: 2026-08-16T14:12:41Z
    attempt_count: 2
worktree:
  default_ai: claude
  recent_repos:
    - path: /Users/dev/code/devgeta
      last_used: 2026-08-24T18:02:11Z
    - path: /Users/dev/code/api-service
      last_used: 2026-08-23T11:47:32Z
    - path: /Users/dev/code/infra-tools
      last_used: 2026-08-20T08:15:00Z
    - path: /Users/dev/code/dotfiles
      last_used: 2026-08-19T12:30:00Z
    - path: /Users/dev/code/homelab
      last_used: 2026-08-15T09:00:00Z
  search_paths:
    - /Users/dev/code
    - /Users/dev/work
    - /Users/dev/oss
    - /Users/dev/sandbox
  scan_depth: 4
  default_layout: main-vertical
  attach_after_create: true
  notify_sound: true
integrations:
  rtk_claude_hook: true
review:
  reviewers:
    - anthropic/claude-opus-4-6
    - anthropic/claude-sonnet-4-6
  rounds: 3
`

// BenchmarkLoad measures the real, current cost of (*GlobalConfig).Load() -
// a bare os.ReadFile followed by yaml.Unmarshal, with no cache anywhere in
// internal/config (ADR-0030). It exists to put a measured number behind
// ADR-0030's "Considered and rejected - doing nothing" section, which
// otherwise hand-waves the per-call cost of the ~95 non-test Load() call
// sites as "a few milliseconds each" with nothing benchmarked behind that
// phrase.
//
// This must be run and its result written into the ADR before Task 5 (the
// cache in front of Load()) lands - the point is to measure Load() as it
// behaves today, uncached, not the fixed version.
//
// b.N amortizes the single cold first iteration's page-fault noise across N
// runs, which is what the cycle's plan calls "warm, best-of-N": a standard
// `for i := 0; i < b.N; i++` loop already satisfies that without
// reproducing ADR-0030's own best-of-5/200-iteration harness style.
func BenchmarkLoad(b *testing.B) {
	tempDir := b.TempDir()
	origConfigRoot := paths.Paths.Config.Root
	paths.Paths.Config.Root = filepath.Join(tempDir, "config")
	b.Cleanup(func() {
		paths.Paths.Config.Root = origConfigRoot
	})

	configPath := GlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		b.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(representativeGlobalConfigYAML), 0o644); err != nil {
		b.Fatalf("failed to write fixture config file: %v", err)
	}

	b.ReportAllocs()
	gc := &GlobalConfig{}
	for i := 0; i < b.N; i++ {
		if err := gc.Load(); err != nil {
			b.Fatalf("Load failed: %v", err)
		}
	}
}
