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

const (
	// App names
	Aerospace    = "aerospace"
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
	Shared             = "shared"
	OpenSSL            = "openssl"
	PkgConfig          = "pkg-config"
	Powerlevel10k      = "powerlevel10k"
	Raycast            = "raycast"
	Readline           = "readline"
	Ripgrep            = "rg"
	Rtk                = "rtk"
	Syntaxhighlighting = "zsh-syntax-highlighting"
	Tldr               = "tldr"
	Tmux               = "tmux"
	TreeSitterCli      = "tree-sitter-cli"  // Same name on both Homebrew and apt (trixie/sid)
	Markdownlint       = "markdownlint-cli" // Brew/npm package; binary is `markdownlint`
	Flake8             = "flake8"
	GolangciLint       = "golangci-lint"
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
