# Governance Self-Modification Defense

This runbook covers the host-level defense for governance files that agents
must not mutate, especially the saved-script bypass that inline command
normalization cannot inspect safely.

## Why This Exists

Chitin now catches inline interpreter attempts such as:

```bash
python3 -c "open('chitin.yaml','w').write('x')"
node -e "fs.writeFileSync('chitin.yaml', 'x')"
bash -c "echo x > chitin.yaml"
```

It intentionally does not read script files from disk at gate time. A command
like `python3 /tmp/write_policy.py` can race between gate evaluation and script
execution, so static script inspection is not a reliable kernel defense.

Use Linux immutable attributes as the operator-side defense until signed-policy
verification lands.

## One-Time Setup

Run these on each host that runs governed agents:

```bash
sudo chattr +i /home/red/workspace/chitin/chitin.yaml
sudo chattr +i -R /home/red/.chitin/
sudo chattr +i -R /home/red/.hermes/plugins/chitin-governance/
```

Verify the immutable bit:

```bash
lsattr /home/red/workspace/chitin/chitin.yaml
lsattr -d /home/red/.chitin /home/red/.hermes/plugins/chitin-governance
```

The output should include `i` in the attribute column.

## Legitimate Operator Edit

Only the operator should use this flow:

```bash
sudo chattr -i /home/red/workspace/chitin/chitin.yaml
$EDITOR /home/red/workspace/chitin/chitin.yaml
sudo chattr +i /home/red/workspace/chitin/chitin.yaml
git -C /home/red/workspace/chitin add chitin.yaml
git -C /home/red/workspace/chitin commit -S -m "policy: <change>"
```

Branch and PR creation follow the normal operator-edit workflow.

## Rollback

Remove the immutable bit before uninstalling, migrating, or repairing files:

```bash
sudo chattr -i /home/red/workspace/chitin/chitin.yaml
sudo chattr -i -R /home/red/.chitin/
sudo chattr -i -R /home/red/.hermes/plugins/chitin-governance/
```

## Residual Gaps

Root can remove immutable attributes. This is a defense against rogue agent
tool calls under the operator account, not a defense against a compromised
root account.

The paths above are host-specific. Add additional governance plugin locations
explicitly if the host uses a different checkout, config home, or plugin path.

`sudo` access is itself a trust boundary. Do not grant agents passwordless
`sudo` for `chattr`, editors, shells, or file-copy commands.

Phase 2 signed-policy verification should replace this as the durable kernel
defense for tampered `chitin.yaml` loads.
