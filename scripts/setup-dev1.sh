#!/usr/bin/env bash
# scripts/setup-dev1.sh — one-shot bootstrap for a Linux test box that
# scripts/ci-dev1.sh can target.
#
# Idempotent. Re-run safely after partial failures. Each phase detects
# what's already in place and skips if so.
#
# Phases:
#   1. (HOST) Configure Incus container — nesting + syscall intercepts.
#      Skip if the target isn't an Incus container, or if config is
#      already applied.
#   2. (CONTAINER) Install Go rootless to ~/.local/go.
#   3. (CONTAINER) Install Docker via apt — needs sudo, prints the
#      command and waits for you to run it in another terminal.
#   4. (CONTAINER) Add user to docker group (via `incus exec` from the
#      host, no sudo prompt).
#   5. (CONTAINER) Install docker buildx rootless to
#      ~/.docker/cli-plugins/.
#   6. (CONTAINER) Clone WithAutonomi/indelible at $REPO_PATH if missing.
#
# Setup expectations:
#   - SSH alias to the container (default: $CONTAINER=dev1) reachable
#     directly from the operator's machine.
#   - SSH alias to the LXC host (default: $HOST=prodesk) reachable, OR
#     skip with --skip-host if the container isn't an Incus container
#     (plain VM, bare metal, cloud instance).
#   - You're in the `incus-admin` group on the host (no sudo needed for
#     `incus config set` or `incus exec`).
#
# Usage:
#   scripts/setup-dev1.sh                   # default: HOST=prodesk CONTAINER=dev1
#   HOST=mybox CONTAINER=mybox-lxc scripts/setup-dev1.sh
#   scripts/setup-dev1.sh --skip-host       # not an Incus container, skip phase 1+4
#   scripts/setup-dev1.sh --check           # only report state, don't install

set -uo pipefail

HOST="${HOST:-prodesk}"
CONTAINER="${CONTAINER:-dev1}"
REPO_PATH="${REPO_PATH:-~/dev/indelible}"
GO_VERSION="${GO_VERSION:-1.25.10}"
SKIP_HOST=false
CHECK_ONLY=false

while [ $# -gt 0 ]; do
  case $1 in
    --skip-host) SKIP_HOST=true ;;
    --check) CHECK_ONLY=true ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
  shift
done

say() { echo ""; echo "→ $*"; }
ok()  { echo "  [ok] $*"; }
todo(){ echo "  [todo] $*"; }
fail(){ echo "  [fail] $*"; exit 1; }

# Run cmd on the host (Incus admin), suppress noise from $? checks.
host_run() { ssh "$HOST" "$@"; }
# Run cmd inside the container via SSH (non-interactive).
ctr_run()  { ssh "$CONTAINER" "$@"; }
# Run cmd inside the container as root, via incus exec on the host.
# Side-steps sudo prompts for the few things that need it.
ctr_root() { ssh "$HOST" "incus exec $CONTAINER -- $*"; }

say "Setup config"
echo "  HOST       = $HOST"
echo "  CONTAINER  = $CONTAINER"
echo "  REPO_PATH  = $REPO_PATH"
echo "  GO_VERSION = $GO_VERSION"
echo "  SKIP_HOST  = $SKIP_HOST"
echo "  CHECK_ONLY = $CHECK_ONLY"

# Sanity-check SSH reachability up front so failures are clear.
say "Checking SSH reachability"
if ! ssh -o ConnectTimeout=5 -o BatchMode=yes "$CONTAINER" true 2>/dev/null; then
  fail "Cannot SSH to $CONTAINER. Set up ~/.ssh/config or pass CONTAINER=<alias>."
fi
ok "ssh $CONTAINER works"
if [ "$SKIP_HOST" != "true" ]; then
  if ! ssh -o ConnectTimeout=5 -o BatchMode=yes "$HOST" true 2>/dev/null; then
    fail "Cannot SSH to $HOST. Use --skip-host if $CONTAINER isn't an Incus container, or fix SSH config."
  fi
  ok "ssh $HOST works"
fi

# --- Phase 1: Incus container config ----------------------------------

if [ "$SKIP_HOST" != "true" ]; then
  say "Phase 1: Incus container config"
  current=$(host_run "incus config show $CONTAINER 2>/dev/null | grep -E '^  security\.(nesting|syscalls\.intercept\.(mknod|setxattr))' | tr -d ' '")
  needs_nesting=true
  needs_mknod=true
  needs_setxattr=true
  echo "$current" | grep -q 'security.nesting:"true"' && needs_nesting=false
  echo "$current" | grep -q 'security.syscalls.intercept.mknod:"true"' && needs_mknod=false
  echo "$current" | grep -q 'security.syscalls.intercept.setxattr:"true"' && needs_setxattr=false

  if ! $needs_nesting && ! $needs_mknod && ! $needs_setxattr; then
    ok "Incus config already complete"
  elif [ "$CHECK_ONLY" = "true" ]; then
    $needs_nesting   && todo "set security.nesting=true"
    $needs_mknod     && todo "set security.syscalls.intercept.mknod=true"
    $needs_setxattr  && todo "set security.syscalls.intercept.setxattr=true"
    todo "incus restart $CONTAINER"
  else
    $needs_nesting   && host_run "incus config set $CONTAINER security.nesting=true"           && ok "nesting=true"
    $needs_mknod     && host_run "incus config set $CONTAINER security.syscalls.intercept.mknod=true"    && ok "intercept.mknod=true"
    $needs_setxattr  && host_run "incus config set $CONTAINER security.syscalls.intercept.setxattr=true" && ok "intercept.setxattr=true"
    say "Restarting $CONTAINER (~10s)"
    host_run "incus restart $CONTAINER"
    for _ in $(seq 1 30); do
      ssh -o ConnectTimeout=3 -o BatchMode=yes "$CONTAINER" true 2>/dev/null && break
      sleep 1
    done
    ok "container back up"
  fi
fi

# --- Phase 2: Go install ----------------------------------------------

say "Phase 2: Go ${GO_VERSION} rootless"
have_go=$(ctr_run "test -x \$HOME/.local/go/bin/go && \$HOME/.local/go/bin/go version" 2>/dev/null || true)
if echo "$have_go" | grep -q "go${GO_VERSION}"; then
  ok "$have_go"
elif [ "$CHECK_ONLY" = "true" ]; then
  todo "install go${GO_VERSION} to ~/.local/go"
else
  ctr_run "set -e
    cd /tmp
    curl -fsSL -o go.tar.gz https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
    mkdir -p \$HOME/.local
    rm -rf \$HOME/.local/go
    tar -C \$HOME/.local -xzf go.tar.gz
    rm /tmp/go.tar.gz
    grep -q '.local/go/bin' \$HOME/.profile 2>/dev/null || cat >> \$HOME/.profile <<EOF

# Go installed by setup-dev1.sh
export PATH=\\\$HOME/.local/go/bin:\\\$HOME/go/bin:\\\$PATH
EOF
  "
  ok "$(ctr_run '$HOME/.local/go/bin/go version')"
fi

# --- Phase 3: Docker install (sudo, manual) ---------------------------

say "Phase 3: Docker"
have_docker=$(ctr_run "command -v docker >/dev/null 2>&1 && docker --version" 2>/dev/null || true)
if [ -n "$have_docker" ]; then
  ok "$have_docker"
else
  if [ "$CHECK_ONLY" = "true" ]; then
    todo "install docker.io (needs sudo on $CONTAINER)"
  else
    echo ""
    echo "  Docker install needs sudo on $CONTAINER. Run this in another terminal:"
    echo ""
    echo "    ssh $CONTAINER 'sudo apt-get update && sudo apt-get install -y docker.io && sudo systemctl enable --now docker'"
    echo ""
    read -rp "  Press Enter when Docker is installed, or Ctrl-C to abort: " _
    have_docker=$(ctr_run "docker --version" 2>/dev/null || true)
    [ -n "$have_docker" ] || fail "docker still not installed on $CONTAINER"
    ok "$have_docker"
  fi
fi

# --- Phase 4: Add user to docker group (via incus exec, no sudo) ------

if [ "$SKIP_HOST" != "true" ]; then
  say "Phase 4: Container user in docker group"
  user=$(ctr_run "whoami")
  in_group=$(ctr_run "id -nG | tr ' ' '\n' | grep -c '^docker\$'" || echo 0)
  if [ "$in_group" -gt 0 ]; then
    ok "user $user already in docker group"
  elif [ "$CHECK_ONLY" = "true" ]; then
    todo "add $user to docker group (logout/login required after)"
  else
    host_run "incus exec $CONTAINER -- usermod -aG docker $user"
    ok "added $user to docker group — RESTART CONTAINER for group to apply to new SSH sessions"
    if [ -n "${have_docker:-}" ]; then
      host_run "incus restart $CONTAINER"
      for _ in $(seq 1 30); do
        ssh -o ConnectTimeout=3 -o BatchMode=yes "$CONTAINER" true 2>/dev/null && break
        sleep 1
      done
      ok "container restarted; group should be live"
    fi
  fi
fi

# --- Phase 5: buildx rootless -----------------------------------------

say "Phase 5: Docker buildx plugin (rootless)"
have_buildx=$(ctr_run "docker buildx version 2>/dev/null | head -1" 2>/dev/null || true)
if [ -n "$have_buildx" ]; then
  ok "$have_buildx"
elif [ "$CHECK_ONLY" = "true" ]; then
  todo "install docker buildx to ~/.docker/cli-plugins/"
else
  ctr_run "set -e
    mkdir -p \$HOME/.docker/cli-plugins
    latest=\$(curl -fsSL https://api.github.com/repos/docker/buildx/releases/latest | grep -oP '\"tag_name\": \"\\K[^\"]+' || echo v0.34.0)
    curl -fsSL -o \$HOME/.docker/cli-plugins/docker-buildx \"https://github.com/docker/buildx/releases/download/\$latest/buildx-\$latest.linux-amd64\"
    chmod +x \$HOME/.docker/cli-plugins/docker-buildx
  "
  ok "$(ctr_run 'docker buildx version | head -1')"
fi

# --- Phase 6: Repo clone ----------------------------------------------

say "Phase 6: Repo at $REPO_PATH"
expanded=$(ctr_run "eval echo $REPO_PATH")
exists=$(ctr_run "test -d $expanded/.git && echo yes || echo no")
if [ "$exists" = "yes" ]; then
  ok "repo already at $expanded"
  ctr_run "cd $expanded && git fetch origin 2>&1 | tail -3"
elif [ "$CHECK_ONLY" = "true" ]; then
  todo "clone WithAutonomi/indelible to $expanded"
else
  parent=$(dirname "$expanded")
  ctr_run "mkdir -p $parent && git clone https://github.com/WithAutonomi/indelible.git $expanded"
  ok "cloned to $expanded"
fi

# --- Summary ----------------------------------------------------------

echo ""
echo "======================="
if [ "$CHECK_ONLY" = "true" ]; then
  echo "[check] State reported above. Re-run without --check to apply."
else
  echo "[ok] $CONTAINER is ready for: make ci-dev1"
  echo ""
  echo "  Try a smoke run:"
  echo "    CONTAINER=$CONTAINER REPO_PATH=$REPO_PATH bash scripts/ci-dev1.sh --only race"
fi
