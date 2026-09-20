# Docker

Devgeta installs [Docker Desktop](https://www.docker.com/products/docker-desktop/)
as a desktop app, and [lazydocker](https://github.com/jesseduffield/lazydocker)
(aliased `lzd`) as a TUI over the same daemon.

- **Module:** `internal/apps/docker/`
- **Install:** `brew install --cask docker`
- **Configuration:** none deployed — Docker Desktop's GUI or `~/.docker/daemon.json`

## Start it

The `docker` CLI does nothing until Docker Desktop is running. Open it from
Applications (or `Cmd+Space` → "Docker"), then:

```bash
docker version
docker ps
```

## Commands

```bash
docker run hello-world        # run a container
docker ps                     # running
docker ps -a                  # all, including stopped
docker pull ubuntu
docker build -t my-app .
docker logs -f <container>    # follow output

docker exec -it <container> bash                  # shell into a running container
docker run -it --entrypoint=/bin/bash <image>     # shell into an image whose entrypoint crashes
```

## Reclaim disk space

```bash
docker system df                     # what's using space
docker system prune                  # stopped containers, unused networks, dangling images
docker system prune -a --volumes     # also unused images and volumes
```

Nuclear versions — these delete unbacked-up volume data:

```bash
docker rm -f $(docker ps -aq)
docker rmi -f $(docker images -aq)
docker volume rm -f $(docker volume ls -q)
```

## Kubernetes

Docker Desktop runs a single-node cluster and bundles its own `kubectl`
(**Settings → Kubernetes → Enable Kubernetes**). Devgeta does not install
`kubectl`, so install it separately on Linux or for a remote cluster.

```bash
kubectl config get-contexts                            # clusters you can reach
kubectl config use-context <context>
kubectl config set-context --current --namespace=<ns>  # stop typing -n

kubectl get pods
kubectl get pod -l service=<name>       # by label, better than grep
kubectl describe pod <pod>              # events, image, restart reasons

kubectl logs --follow <pod>
kubectl logs --previous <pod>           # the crashed container, not the restarted one
kubectl exec -it <pod> -- bash
```

`--previous` is the one for a `CrashLoopBackOff` — the current container just
started; the reason it died is in the previous one.

## Uninstall

`dg uninstall docker` removes the cask and the `global_config.yaml` entry, but
leaves daemon data, binaries, and completions. Full cleanup, after quitting
Docker Desktop from the menu bar:

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
