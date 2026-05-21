# Slack reply to Tarek — gstack research

> Paste directly into Tarek's thread. Tone matches the existing thread (casual, founder-to-founder, not overly polished).

---

Did a real dig — gstack is the right read. Posting a brief recap; full writeup is at `chitin/docs/readybench/gstack-research-2026-05-18.md` in my notes (~5 pages, can share whenever).

**Short version:** install it. Use the `/office-hours` skill (YC's 6 forcing questions: demand reality, status quo, desperate specificity, narrowest wedge, observation, future-fit) as the discovery layer when a customer creates a ResourceRequest. Output gets attached to the request as a structured spec doc. That spec — not the freeform `description` field — drives matching.

**3 integration paths in the brief:**
1. **Lightweight (1-2 wk):** discovery wizard, output as attachment. No matching-engine changes. Best for week-1 learning.
2. **Medium (3-4 wk):** spec as a first-class entity; matching reads it instead of `description`. Better match quality.
3. **Heavy (8-12 wk):** gstack personas across every customer touch. Right end-state if Paths 1+2 prove value.

**My rec:** ship Path 1 in two weeks, measure for a month, gate Path 2 on conversion lift ≥ 15% OR mismatch-cancellation drop ≥ 25%.

**Want to do a 30-min sync this week?** I'll bring a UX sketch of the Path 1 wizard + a measurement framework. You bring current conversion + churn baselines. We decide go/no-go in that meeting.

(Side note: gstack is MIT-licensed and Anthropic-tied — the only real vendor concentration is on Claude Code as the runtime. Worth flagging for William too. I'll loop him in on the sync if useful.)
