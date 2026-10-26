#!/usr/bin/env bash
# Runs inside a desk (sent by Shortdesk). Reports and merges desks' work on projects in
# ~/Developer. Everything is local git; nothing is pushed.
#
#   status               one JSON line per project: this desk's branch vs main
#   diff <project>       this desk's changes (committed on its branch) against main
#   merge <project>      merge this desk's branch into main; on conflict, undo and report
set -euo pipefail

dev="$HOME/Developer"
desk=$(cat "$HOME/.shortdesk-desk" 2>/dev/null || hostname)
desk=$(printf %s "$desk" | tr -c 'A-Za-z0-9-' '-')
branch="desk/$desk"

main_of() { git -C "$1" symbolic-ref --quiet --short HEAD 2>/dev/null || echo main; }

status() {
  for d in "$dev"/*/; do
    [ -d "$d.git" ] || continue
    local name repo main ahead=0 behind=0 last="" dirty=0
    name=$(basename "$d"); repo="${d%/}"; main=$(main_of "$repo")
    if git -C "$repo" show-ref --verify --quiet "refs/heads/$branch"; then
      ahead=$(git -C "$repo" rev-list --count "$main..$branch")
      behind=$(git -C "$repo" rev-list --count "$branch..$main")
      last=$(git -C "$repo" log -1 --format=%s "$branch")
      [ -d "$repo/.desks/$desk" ] && dirty=$(git -C "$repo/.desks/$desk" status --porcelain | wc -l | tr -d ' ')
      jq -cn --arg name "$name" --arg main "$main" --arg branch "$branch" --arg last "$last" \
        --argjson ahead "$ahead" --argjson behind "$behind" --argjson dirty "$dirty" \
        '{name:$name, main:$main, branch:$branch, ahead:$ahead, behind:$behind, uncommitted:$dirty, lastCommit:$last}'
    else
      jq -cn --arg name "$name" --arg main "$main" '{name:$name, main:$main, branch:"", ahead:0, behind:0, uncommitted:0, lastCommit:""}'
    fi
  done
}

valid() { [[ "$1" =~ ^[A-Za-z0-9._-]+$ && "$1" != .* && -d "$dev/$1/.git" ]] || { echo "no project $1" >&2; exit 2; }; }

case "${1:-}" in
  status) status ;;
  diff)
    valid "${2:-}"
    repo="$dev/$2"; main=$(main_of "$repo")
    git -C "$repo" diff --stat "$main...$branch"
    echo
    git -C "$repo" diff "$main...$branch" | head -c 300000
    ;;
  merge)
    valid "${2:-}"
    repo="$dev/$2"; main=$(main_of "$repo")
    if [ -n "$(git -C "$repo" status --porcelain --untracked-files=no)" ]; then
      echo "the project's main copy has uncommitted changes; merge them or discard them first" >&2
      exit 3
    fi
    if git -C "$repo" merge --no-ff -q -m "Merge $branch into $main" "$branch" >/dev/null 2>&1; then
      echo "merged"
    else
      conflicts=$(git -C "$repo" diff --name-only --diff-filter=U | tr '\n' ' ')
      git -C "$repo" merge --abort 2>/dev/null || true
      echo "conflict in: ${conflicts:-unknown files}" >&2
      exit 4
    fi
    ;;
  *) echo "usage: status | diff <project> | merge <project>" >&2; exit 2 ;;
esac
