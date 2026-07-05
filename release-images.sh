#!/usr/bin/env bash
# One-shot release: create the GitHub repo (first run only), push the
# go-rewrite branch, and follow the image build to completion.
#
# One-time prerequisite (interactive browser login):
#   gh auth login -s "repo,workflow,write:packages"
set -euo pipefail
cd "$(dirname "$0")"

OWNER="${OWNER:-b-mian}"
REPO="${REPO:-misarch-go}"

if ! gh auth status >/dev/null 2>&1; then
  echo 'GitHub CLI is not authenticated. Run:'
  echo '  gh auth login -s "repo,workflow,write:packages"'
  exit 1
fi

if ! gh repo view "$OWNER/$REPO" >/dev/null 2>&1; then
  echo "Creating private repo $OWNER/$REPO ..."
  gh repo create "$OWNER/$REPO" --private \
    --description "MiSArch business services rewritten in Go (energy-efficiency study)"
fi

git remote get-url gh >/dev/null 2>&1 || git remote add gh "https://github.com/$OWNER/$REPO.git"
git push -u gh go-rewrite

echo "Waiting for the build-images workflow to register..."
sleep 10
run_id=$(gh run list -R "$OWNER/$REPO" --workflow=build-images --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch -R "$OWNER/$REPO" "$run_id" --exit-status

cat <<EOF

Done: ghcr.io/$OWNER/misarch-go:<service> for all 17 services
(immutable copies at :<service>-$(git rev-parse HEAD)).

The GHCR package starts PRIVATE. Pick one:
  a) Make it public (compiled binaries only; the source repo stays private):
     https://github.com/users/$OWNER/packages/container/misarch-go/settings
     -> Danger Zone -> Change visibility -> Public
  b) Keep it private: create a PAT with read:packages and pass it to
     terraform via TF_VAR_GHCR_PULL_TOKEN so the cluster can pull
     (see variables-images.tf in the k8s repo).
EOF
