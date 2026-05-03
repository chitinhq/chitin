# Peer-reviewer structured review template

Post your review with this exact body shape so the gatekeeper's
parser keys on it cleanly:

```
### peer-reviewer findings

🔴 (real bug) findings:
- <path>:<line> — <one-paragraph description; cite the line, name
  the invariant violation, propose the fix>

🟡 (worth a second look) findings:
- <path>:<line> — <one-paragraph description; explain the concern
  and what would resolve it>

🟢 (nice-to-have, non-blocking) findings:
- <path>:<line> — <brief>

Overall: <APPROVE | REQUEST_CHANGES | OBSERVE>
- APPROVE: zero 🔴 + acceptable 🟡 count
- REQUEST_CHANGES: any 🔴
- OBSERVE: complexity merits a second tier reviewer (R2/R3)
```

Skip the 🟢 section entirely if you have none. Don't pad the review.

Findings R0 (Copilot) already flagged: include them in counts, but
annotate "(also flagged by R0)" in the description so the operator
can see overlap at a glance.
