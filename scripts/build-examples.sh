#!/bin/sh
# Rebuilds examples/buildinfo/orders-*.txt from examples/orders.
#
# It commits each version of the made-up orders service to a throwaway git
# repository, builds it the way a release pipeline or a laptop would, and
# saves `go version -m` output. Needs git, and the network the first time to
# download the dependencies into the module cache.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
src="$repo/examples/orders"
out="$repo/examples/buildinfo"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

export GOTOOLCHAIN=local GOFLAGS=-mod=mod
git_commit() {
	git -C "$work/orders" add -A
	GIT_AUTHOR_DATE="$1" GIT_COMMITTER_DATE="$1" \
		git -C "$work/orders" -c user.name=example -c user.email=example@example.com \
		commit -q -m "$2"
}
save() {
	# Name the binary "orders" in the header, whatever the temp path was.
	go version -m "$work/orders/orders" | sed '1s|^[^:]*:|orders:|' > "$out/$1"
	echo "wrote examples/buildinfo/$1"
}
release_build() {
	(cd "$work/orders" && CGO_ENABLED=0 go build -trimpath -tags netgo \
		-ldflags "-s -w -X main.version=$1" -o orders .)
}

mkdir -p "$work/orders" "$out"
git -C "$work/orders" init -q
printf 'orders\n' > "$work/orders/.gitignore"

# v1.4.0: the release in production, built by CI.
cp "$src/before/go.mod" "$src/before/go.sum" "$src/before/main.go" "$work/orders/"
git_commit 2026-10-20T10:00:00Z "orders v1.4.0"
git -C "$work/orders" tag v1.4.0
release_build v1.4.0
save orders-v1.4.0.txt

# v1.4.1: a comment-only change, built by CI the same way.
cp "$src/comments/main.go" "$work/orders/"
git_commit 2026-10-27T10:00:00Z "Document the version variable"
git -C "$work/orders" tag v1.4.1
release_build v1.4.1
save orders-v1.4.1.txt

# hotfix: built on a laptop from uncommitted changes, with a local fork of
# github.com/google/uuid next to the repository.
cp -R "$(go env GOMODCACHE)/github.com/google/uuid@v1.6.0" "$work/uuid-fork"
chmod -R u+w "$work/uuid-fork"
cp "$src/hotfix/go.mod" "$src/hotfix/go.sum" "$src/hotfix/main.go" "$work/orders/"
(cd "$work/orders" && go build -ldflags "-X main.version=v1.4.1-hotfix" -o orders .)
save orders-hotfix.txt
