package constants

const (
	Reset  = "\033[0m"
	Gray   = "\033[90m" // Muted color
	Red    = "\033[31m" // Error color
	Green  = "\033[32m" // Success color
	Blue   = "\033[34m" // Informative color
	Yellow = "\033[33m" // Warning color
	Bold   = "\033[1m"
)

// HomebrewPrefixes are the two prefixes Homebrew installs itself into on
// macOS: /opt/homebrew on Apple Silicon, /usr/local on Intel. Anything that
// has to locate Homebrew's own files without asking `brew` — because `brew`
// may not be on PATH yet, or at all — checks both, in this order.
var HomebrewPrefixes = []string{"/opt/homebrew", "/usr/local"}

type OsVersion struct {
	Number int
	Name   string
}

type AppVersion struct {
	Number string
}

// SupportedVersion contains version requirements for different platforms and applications
var SupportedVersion = struct {
	MacOS  OsVersion
	Debian OsVersion
	Ubuntu OsVersion
	Neovim AppVersion
}{
	MacOS: OsVersion{
		Number: 13,
		Name:   "Ventura",
	},
	Debian: OsVersion{
		Number: 12,
		Name:   "Bookworm",
	},
	Ubuntu: OsVersion{
		Number: 24,
		Name:   "Noble Numbat",
	},
	Neovim: AppVersion{
		Number: "0.11.1",
	},
}

// PinnedVersion is the exact version devgeta installs of each AI coder. A
// coder release can change what devgeta's config means (OpenCode 2 dropped
// half of the 1.x keys without an error), so devgeta installs the version its
// config was checked against and turns the coder's self-update off. Moving a
// pin is a devgeta change: bump it here, read the release notes against the
// config devgeta writes, check it in a sandbox HOME, and release.
var PinnedVersion = struct {
	Claude   string
	OpenCode string
	// Pi is a reference, not a pin: Pi's official installer (ADR-0059) always
	// installs the latest release and takes no version. This is the release
	// devgeta's Pi config was last checked against.
	Pi string
}{
	Claude:   "2.1.319",
	OpenCode: "2.2.6",
	Pi:       "1.5.0",
}

// App contains application-specific constants
var App = struct {
	Name       string
	Repository struct {
		Name string
		URL  string
	}
	Dir struct {
		Configs string
	}
	File struct {
		GlobalConfig string
		ZshConfig    string
	}
	Template struct {
		ShellConfig string
	}
}{
	Name: "devgeta",
	Repository: struct {
		Name string
		URL  string
	}{
		Name: "devgeta",
		URL:  "https://github.com/cjairm/devgeta.git",
	},
	Dir: struct {
		Configs string
	}{
		Configs: "configs",
	},
	File: struct {
		GlobalConfig string
		ZshConfig    string
	}{
		GlobalConfig: "global_config.yaml",
		ZshConfig:    ".devgeta.zsh",
	},
	Template: struct {
		ShellConfig string
	}{
		ShellConfig: "devgeta.zsh.tmpl",
	},
}

// AgentPackMarketplace is the name of the Claude Code marketplace devgeta
// registers for the agent packs it builds. Claude Code ids a plugin as
// "<pack>@<marketplace>".
const AgentPackMarketplace = "devgeta"

const (
	// App names
	Aerospace    = "aerospace"
	AgentPacks   = "agent-packs"
	Alacritty    = "alacritty"
	Autoconf     = "autoconf"
	Bison        = "bison"
	Bat          = "bat"
	Brave        = "brave"
	BraveBrowser = "brave-browser"
	Btop         = "btop"
	Curl         = "curl"
	Jq           = "jq"
	// TODO: Rename this to `Devgeta` and update current Devgeta graphic to match the new name. (This is to avoid confusion between the app and the repository)
	DevgetaApp         = "devgeta"
	Docker             = "docker"
	Eza                = "eza"
	Fastfetch          = "fastfetch"
	FdFind             = "fd" // Equivalent to "fd-find" in Linux package managers
	FontConfig         = "fontconfig"
	Flameshot          = "flameshot"
	Fzf                = "fzf"
	Gcc                = "gcc"
	Gdbm               = "gdbm"
	Ghostty            = "ghostty"
	Gimp               = "gimp"
	Git                = "git"
	GithubCli          = "gh"
	I3                 = "i3"
	Jemalloc           = "jemalloc"
	LazyDocker         = "lazydocker"
	LazyGit            = "lazygit"
	Libffi             = "libffi"
	Libyaml            = "libyaml"
	Make               = "make"
	Mupdf              = "mupdf"
	Ncurses            = "ncurses"
	Neovim             = "neovim"
	Mise               = "mise"
	Nvim               = "nvim"
	Claude             = "claude"
	OpenCode           = "opencode"
	Pi                 = "pi"
	OpenSSL            = "openssl"
	PkgConfig          = "pkg-config"
	Powerlevel10k      = "powerlevel10k"
	Raycast            = "raycast"
	Readline           = "readline"
	Ripgrep            = "rg"
	Shottr             = "shottr"
	Syntaxhighlighting = "zsh-syntax-highlighting"
	Tldr               = "tldr"
	Tmux               = "tmux"
	TreeSitterCli      = "tree-sitter-cli"  // Same name on both Homebrew and apt (trixie/sid)
	Markdownlint       = "markdownlint-cli" // Brew/npm package; binary is `markdownlint`
	Flake8             = "flake8"
	GolangciLint       = "golangci-lint"
	Handy              = "handy"
	Ulauncher          = "ulauncher"
	Unzip              = "unzip"
	ApacheUtils        = "apache2-utils"
	Plocate            = "plocate"
	Vips               = "vips"
	Xclip              = "xclip"
	Xcode              = "xcode"
	Zlib               = "zlib"
	Zoxide             = "zoxide"
	ZshAutosuggestions = "zsh-autosuggestions"

	// Other available folders for installation
	Fonts     = "fonts"
	Terminal  = "terminal"
	Themes    = "themes"
	Templates = "templates"

	// Programming languages (Mise core tools)
	Bun    = "bun"
	Deno   = "deno"
	Elixir = "elixir"
	Erlang = "erlang"
	Go     = "go"
	Java   = "java"
	Node   = "node"
	Python = "python"
	Ruby   = "ruby"
	Rust   = "rust"

	// Programming languages (native package manager)
	PHP = "php"

	// Databases (native package manager)
	MongoDB    = "mongodb"
	MySQL      = "mysql"
	PostgreSQL = "postgresql"
	Redis      = "redis"
	SQLite     = "sqlite"
)

var Devgeta = `
    .___                        __
  __| _/_______  ______   _____/  |______
 / __ |/ __ \  \/ / ___\_/ __ \   __\__  \
/ /_/ \  ___/\   / /_/  >  ___/|  |  / __ \_
\____ |\___  >\_/\___  / \___  >__| (____  /
     \/    \/   /_____/      \/          \/
@cjairm / https://cjairm.me/
`
