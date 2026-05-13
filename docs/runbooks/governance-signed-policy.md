# Signed Governance Policy Runbook

Chitin can verify `chitin.yaml` before loading it. When the operator public
key is pinned in `~/.chitin/trust/`, a missing or invalid `chitin.yaml.sig`
fails closed: the gate denies the tool call and writes a structured
`policy_signature_invalid` decision row.

## Files

- Policy: `chitin.yaml`
- Signature sidecar: `chitin.yaml.sig`
- Operator private key: `~/.chitin/trust/chitin-policy-ed25519`
- Operator public key: `~/.chitin/trust/chitin-policy-ed25519.pub`

The private key is operator-local and must not be committed. The public key is
safe to copy into CI variable `CHITIN_POLICY_PUBLIC_KEY` and onto governed
hosts.

## Generate Keys

```bash
mkdir -p ~/.chitin/trust
chmod 700 ~/.chitin ~/.chitin/trust
chitin-kernel policy keygen \
  --public-out ~/.chitin/trust/chitin-policy-ed25519.pub \
  --private-out ~/.chitin/trust/chitin-policy-ed25519
chmod 600 ~/.chitin/trust/chitin-policy-ed25519
```

Sign the current policy:

```bash
chitin-kernel policy sign \
  --policy-file /home/red/workspace/chitin/chitin.yaml \
  --private-key ~/.chitin/trust/chitin-policy-ed25519
```

Verify:

```bash
chitin-kernel policy verify \
  --policy-file /home/red/workspace/chitin/chitin.yaml \
  --trust-dir ~/.chitin/trust
```

## Operator Workflow

Install the pre-commit signer once per checkout:

```bash
scripts/install-governance-policy-signing-hook.sh
```

When `chitin.yaml` is staged, the hook refreshes and stages
`chitin.yaml.sig` before the commit is created.
CI rejects unsigned policy edits and points back to this runbook. CI reads the
public key from repository variable `CHITIN_POLICY_PUBLIC_KEY`; it never needs
the private key.

## Rotation

Rotate yearly, after suspected compromise, or after an operator handoff:

1. Generate a new keypair in `~/.chitin/trust/`.
2. Copy the new public key into CI variable `CHITIN_POLICY_PUBLIC_KEY`.
3. Re-sign `chitin.yaml` and commit `chitin.yaml.sig`.
4. Deploy the new public key to each host's `~/.chitin/trust/`.
5. Remove the old private key after all hosts verify with the new public key.

## Recovery

If the private key is lost, generate a new pair, update CI with the new public
key, re-sign `chitin.yaml`, and deploy the new public key to governed hosts.
Until that lands, the gate fails closed wherever the old public key is pinned
and the signature cannot be refreshed.

## Break Glass

For emergency recovery only:

```bash
chitin-kernel gate evaluate --hook-stdin --bypass-sig --agent=claude-code
```

or for direct probes:

```bash
chitin-kernel gate evaluate --tool Read --args-json '{"file_path":"chitin.yaml"}' \
  --agent operator --cwd /home/red/workspace/chitin --bypass-sig
```

Every bypass emits `policy_signature_bypass` to stderr and appends an audit row.
Use it only long enough to land a corrected `chitin.yaml.sig`; the warning will
repeat on each bypassed gate evaluation.

## Tamper Regression

```bash
cp chitin.yaml /tmp/chitin.yaml.good
printf '\n# tamper\n' >> chitin.yaml
printf '{"tool_name":"Read","tool_input":{"file_path":"README.md"},"cwd":"%s"}\n' "$PWD" |
  chitin-kernel gate evaluate --hook-stdin --agent=claude-code
```

Expected: exit `2`, stdout block reason containing `policy_signature_invalid`,
and a `gov-decisions-YYYY-MM-DD.jsonl` row with
`"rule_id":"policy_signature_invalid"`.
