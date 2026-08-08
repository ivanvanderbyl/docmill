# Toolchain paths for the spike. Source this before any go command.
#
# The caches live on DISK, not under /tmp. /tmp here is a 7.9 GB tmpfs — it is
# RAM, it fills, and it gets purged under memory pressure. The module cache
# lived there once and its partial loss produced builds that failed with
# "Forbidden" (the sandbox blocks proxy.golang.org) and, worse, one build whose
# failure was masked by a pipeline and left a STALE binary that emitted an
# entire diagnosis dataset without the field being diagnosed.
#
# GOPROXY falls back to the host user's intact download cache, which is a valid
# file-based module proxy. Every dependency is pinned in go.sum, so no sumdb
# access is needed and the blocked network never comes into play.
export PATH=/home/orca/goroot/bin:$PATH
export GOCACHE=/home/orca/doclaynet-work/gocache
export GOMODCACHE=/home/orca/doclaynet-work/gomodcache
export GOPROXY=file:///home/orca/go/pkg/mod/cache/download,https://proxy.golang.org
