---
name: verification-before-completion
description: Use when about to claim work is complete, fixed, or passing, before committing or creating PRs - requires running verification commands and confirming output before making any success claims; evidence before assertions always
---

# Verification Before Completion

**Core principle:** evidence before claims. If you haven't run the verification
command in this message, you can't say it passes.

The rule covers paraphrases and implications, not just the exact words —
"looks good", "should be fine", and an unqualified "Done!" are all completion
claims.

## The gate

Before stating any status: identify the command that would prove the claim, run
it in full, read the entire output including the exit code, and only then state
the claim together with what the output showed. If the output contradicts the
claim, report the actual state instead.

## What proves what

| Claim | Requires | Not sufficient |
|-------|----------|----------------|
| Tests pass | Test command output: 0 failures | A previous run, "should pass" |
| Linter clean | Linter output: 0 errors | A partial check, extrapolation |
| Build succeeds | Build command: exit 0 | Linter passing, logs looking good |
| Bug fixed | The original symptom retested, passing | Code changed, assumed fixed |
| Regression test works | Red-green cycle verified | The test passing once |
| Agent completed | VCS diff showing the changes | The agent reporting "success" |
| Requirements met | Line-by-line checklist against the spec | Tests passing |

Two of these are easy to get wrong:

**Regression tests** need the full cycle — write it, run it (passes), revert
the fix, run it again (it *must* fail), restore the fix, run it once more. A
regression test that was never seen to fail doesn't prove it guards anything.

**Delegated work** needs independent confirmation. A subagent reporting success
is a claim, not evidence; check the diff yourself.
