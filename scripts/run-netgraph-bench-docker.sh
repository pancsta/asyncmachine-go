#!/usr/bin/env bash
# Runs the bench_netgraph tests in an Arch Linux container with the fd limits
# required by the 1M-node full-UDP test.
#
# Usage:
#   scripts/run-netgraph-bench-docker.sh [TestName]
#   AM_NETGRAPH_NODES=200000 AM_NETGRAPH_ITERS=2 \
#     scripts/run-netgraph-bench-docker.sh TestNetGraphFullUDP
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# nearest ancestor holding the sibling dev modules
OSRC=$(cd "$ROOT" && while [ "$PWD" != "/" ] &&
  { [ ! -d "$PWD/livepage" ] || [ ! -d "$PWD/cview" ]; }; do
  cd ..
done; pwd)
TASKBOT=$(cd "$OSRC/../blogic/taskbot/taskbot" 2>/dev/null && pwd || true)
TEST=${1:-TestNetGraphFullUDP}

IMAGE=${IMAGE:-archlinux:latest}
NODES=${AM_NETGRAPH_NODES:-1048575}
ITERS=${AM_NETGRAPH_ITERS:-3}
DEPTH=${AM_NETGRAPH_DEPTH:-}
# 2 fds per node (listener + EmitToAddr sink) + margin
NOFILE=${AM_NETGRAPH_NOFILE:-$((NODES * 2 + 1024))}

GOROOT=${GOROOT:-/usr/lib/go}
GOMODCACHE=${GOMODCACHE:-$(go env GOMODCACHE 2>/dev/null || echo "$HOME/go/pkg/mod")}

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker pull "$IMAGE"
fi

echo "test=$TEST nodes=$NODES iters=$ITERS nofile=$NOFILE"
echo "source=$ROOT"
echo "go=$GOROOT modcache=$GOMODCACHE"

ENV_ARGS=(
  -e "GOROOT=$GOROOT"
  -e GOPATH=/go
  -e "GOMODCACHE=/go/pkg/mod"
  -e GOCACHE=/tmp/gocache
  -e "GOWORK=$ROOT/go.work"
  -e CGO_ENABLED=0
  -e HOME=/tmp
  -e PATH="$GOROOT/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
  -e "AM_NETGRAPH_NODES=$NODES"
  -e "AM_NETGRAPH_ITERS=$ITERS"
)
if [ -n "$DEPTH" ]; then
  ENV_ARGS+=(-e "AM_NETGRAPH_DEPTH=$DEPTH")
fi

MOUNTS=(
  -v "$ROOT":"$ROOT":ro
  -v "$OSRC/livepage/livepage":"$OSRC/livepage/livepage":ro
  -v "$OSRC/cview/cview":"$OSRC/cview/cview":ro
  -v "$OSRC/secai/secai":"$OSRC/secai/secai":ro
)
if [ -n "$TASKBOT" ]; then
  MOUNTS+=(-v "$TASKBOT":"$TASKBOT":ro)
fi

exec docker run --rm --network=host \
  --ulimit "nofile=$NOFILE:$NOFILE" \
  --pids-limit=-1 \
  "${MOUNTS[@]}" \
  -v "$GOROOT":"$GOROOT":ro \
  -v "$GOMODCACHE":/go/pkg/mod:rw \
  "${ENV_ARGS[@]}" \
  -w "$ROOT" \
  "$IMAGE" \
  bash -lc "ulimit -n && go test -count=1 -tags bench_netgraph -run '$TEST' -v -timeout 40m ./pkg/graph/test/"
