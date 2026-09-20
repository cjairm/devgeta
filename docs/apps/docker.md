# Docker Desktop

Devgeta installs [Docker Desktop](https://www.docker.com/products/docker-desktop/),
the containerization platform for building, shipping, and running distributed
applications.

- **Module:** `internal/apps/docker/`

## After Installation

**Start Docker Desktop:**

Open Docker Desktop from your Applications folder or launch it via Spotlight (`Cmd+Space` → "Docker").

**Verify Docker is running:**

```bash
docker version
docker ps
```

**Common commands:**

```bash
# Run a container
docker run hello-world

# List running containers
docker ps

# List all containers
docker ps -a

# Pull an image
docker pull ubuntu

# Build from Dockerfile
docker build -t my-app .
```

## Uninstall (Manual)

Docker Desktop requires manual cleanup. Follow these steps:

**Step 1 — Quit Docker Desktop:**

Right-click the Docker icon in the menu bar and select "Quit Docker Desktop."

**Step 2 — Uninstall via Homebrew (macOS):**

```bash
brew uninstall --cask docker
```

**Step 3 — Remove binaries:**

```bash
sudo rm -f /usr/local/bin/docker
sudo rm -f /usr/local/bin/docker-compose
sudo rm -f /usr/local/bin/docker-credential-desktop
sudo rm -f /usr/local/bin/docker-credential-ecr-login
sudo rm -f /usr/local/bin/docker-credential-osxkeychain
sudo rm -f /usr/local/bin/hub-tool
sudo rm -f /usr/local/bin/kubectl.docker
```

**Step 4 — Remove data and configuration:**

```bash
sudo rm -rf ~/Library/Containers/com.docker.docker
sudo rm -rf ~/Library/Application\ Support/Docker\ Desktop
sudo rm -rf ~/.docker
```

**Step 5 — Remove shell completions:**

```bash
sudo rm -f /usr/local/etc/bash_completion.d/docker
sudo rm -f /usr/local/share/zsh/site-functions/_docker
sudo rm -f /usr/local/share/fish/vendor_completions.d/docker.fish
```

Or as a single command:

```bash
brew uninstall --cask docker && \
  sudo rm -f /usr/local/bin/docker* /usr/local/bin/hub-tool /usr/local/bin/kubectl.docker && \
  sudo rm -rf ~/Library/Containers/com.docker.docker \
              ~/Library/Application\ Support/Docker\ Desktop \
              ~/.docker && \
  sudo rm -f /usr/local/etc/bash_completion.d/docker \
             /usr/local/share/zsh/site-functions/_docker \
             /usr/local/share/fish/vendor_completions.d/docker.fish
```
