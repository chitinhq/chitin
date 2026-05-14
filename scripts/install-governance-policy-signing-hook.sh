#!/usr/bin/env bash
set -euo pipefail

# Installs an operator-local pre-commit hook that signs chitin.yaml when the
# policy is staged. The private key stays outside the repo.

REPO="$(git rev-parse --show-toplevel)"
HOOK="$REPO/.git/hooks/pre-commit"
KEY_FILE="${CHITIN_POLICY_PRIVATE_KEY_FILE:-$HOME/.chitin/trust/chitin-policy-ed25519}"
KERNEL="${CHITIN_KERNEL_BIN:-chitin-kernel}"

mkdir -p "$(dirname "$HOOK")"
cat >"$HOOK" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail

REPO="$(git rev-parse --show-toplevel)"
KEY_FILE="${CHITIN_POLICY_PRIVATE_KEY_FILE:-$HOME/.chitin/trust/chitin-policy-ed25519}"
KERNEL="${CHITIN_KERNEL_BIN:-chitin-kernel}"

if ! git -C "$REPO" diff --cached --name-only | grep -qx 'chitin.yaml'; then
  exit 0
fi

if [[ ! -f "$KEY_FILE" ]]; then
  echo "chitin policy signing: private key missing at $KEY_FILE" >&2
  echo "See docs/runbooks/governance-signed-policy.md" >&2
  exit 1
fi

"$KERNEL" policy sign \
  --policy-file "$REPO/chitin.yaml" \
  --private-key "$KEY_FILE" \
  --out "$REPO/chitin.yaml.sig"

git -C "$REPO" add chitin.yaml.sig
echo "chitin policy signing: refreshed chitin.yaml.sig"
HOOK

chmod +x "$HOOK"
cat <<EOF
installed $HOOK
private key: $KEY_FILE
kernel: $KERNEL
EOF
