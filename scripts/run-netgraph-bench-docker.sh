#!/usr/bin/env bash
# Runs the bench_netgraph tests in an Arch Linux container. The full-UDP test
# needs fd limits, the mocked history test needs RAM and disk.
#
# Usage:
#   scripts/run-netgraph-bench-docker.sh [TestName]
#   AM_NETGRAPH_NODES=200000 AM_NETGRAPH_ITERS=2 \
#     scripts/run-netgraph-bench-docker.sh TestNetworkGraphFullUdp
#   AM_NETGRAPH_MACHINES=1048576 AM_NETGRAPH_SHARDS=8 \
#     scripts/run-netgraph-bench-docker.sh TestNetworkGraphMockHistory
#
# The selected test runs sequentially (-p 1 -parallel 1); do not run this
# script twice in parallel.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# nearest ancestor holding the sibling dev modules
OSRC=$(cd "$ROOT" && while [ "$PWD" != "/" ] &&
  { [ ! -d "$PWD/livepage" ] || [ ! -d "$PWD/cview" ]; }; do
  cd ..
done; pwd)
TASKBOT=$(cd "$OSRC/../blogic/taskbot/taskbot" 2>/dev/null && pwd || true)
TEST=${1:-TestNetworkGraphFullUdp}

case "$TEST" in
  *Mock*) MOCK=1 ;;
  *) MOCK=0 ;;
esac

IMAGE=${IMAGE:-archlinux:latest}
NODES=${AM_NETGRAPH_NODES:-1048575}
ITERS=${AM_NETGRAPH_ITERS:-3}
DEPTH=${AM_NETGRAPH_DEPTH:-}

# mocked history test
MACHINES=${AM_NETGRAPH_MACHINES:-1048576}
STAR=${AM_NETGRAPH_STAR:-16}
SHARDS=${AM_NETGRAPH_SHARDS:-8}
DUR=${AM_NETGRAPH_DUR:-1m}
ACTIVE=${AM_NETGRAPH_ACTIVE:-10000}
LOSS=${AM_NETGRAPH_LOSS:-0.05}
REG_BATCH=${AM_NETGRAPH_REG_BATCH:-50000}
REG_PAUSE=${AM_NETGRAPH_REG_PAUSE:-20ms}

if [ "$MOCK" = 1 ]; then
  # no sockets, only a small ulimit margin
  NOFILE=${AM_NETGRAPH_NOFILE:-65536}
  TIMEOUT=${AM_NETGRAPH_TIMEOUT:-120m}
else
  # 2 fds per node (listener + EmitToAddr sink) + margin
  NOFILE=${AM_NETGRAPH_NOFILE:-$((NODES * 2 + 1024))}
  TIMEOUT=${AM_NETGRAPH_TIMEOUT:-40m}
fi

# limit the container to 25% of the host CPUs by default
CPUS=${AM_NETGRAPH_CPUS:-$(awk -v n="$(nproc)" 'BEGIN { printf "%.2f", n * 0.25 }')}

# limit the container to 50% of the host RAM (no swap) by default
MEM_KB=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
MEM=${AM_NETGRAPH_MEM:-$(awk -v kb="$MEM_KB" 'BEGIN { printf "%dm", kb / 1024 / 2 }')}

GOROOT=${GOROOT:-/usr/lib/go}
GOMODCACHE=${GOMODCACHE:-$(go env GOMODCACHE 2>/dev/null || echo "$HOME/go/pkg/mod")}

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker pull "$IMAGE"
fi

echo "test=$TEST nodes=$NODES iters=$ITERS nofile=$NOFILE timeout=$TIMEOUT cpus=$CPUS mem=$MEM"
echo "machines=$MACHINES star=$STAR shards=$SHARDS dur=$DUR active=$ACTIVE loss=$LOSS"
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
  -e "AM_NETGRAPH_MACHINES=$MACHINES"
  -e "AM_NETGRAPH_STAR=$STAR"
  -e "AM_NETGRAPH_SHARDS=$SHARDS"
  -e "AM_NETGRAPH_DUR=$DUR"
  -e "AM_NETGRAPH_ACTIVE=$ACTIVE"
  -e "AM_NETGRAPH_LOSS=$LOSS"
  -e "AM_NETGRAPH_REG_BATCH=$REG_BATCH"
  -e "AM_NETGRAPH_REG_PAUSE=$REG_PAUSE"
)
if [ -n "$DEPTH" ]; then
  ENV_ARGS+=(-e "AM_NETGRAPH_DEPTH=$DEPTH")
fi
if [ -n "${AM_NETGRAPH_FORCE:-}" ]; then
  ENV_ARGS+=(-e "AM_NETGRAPH_FORCE=$AM_NETGRAPH_FORCE")
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
  --cpus "$CPUS" \
  --memory "$MEM" \
  --memory-swap "$MEM" \
  --ulimit "nofile=$NOFILE:$NOFILE" \
  --pids-limit=-1 \
  "${MOUNTS[@]}" \
  -v "$GOROOT":"$GOROOT":ro \
  -v "$GOMODCACHE":/go/pkg/mod:rw \
  "${ENV_ARGS[@]}" \
  -w "$ROOT" \
  "$IMAGE" \
  bash -lc "ulimit -n && go test -count=1 -tags bench_netgraph -p 1 -parallel 1 -run '$TEST' -v -timeout $TIMEOUT ./pkg/graph/test/"
