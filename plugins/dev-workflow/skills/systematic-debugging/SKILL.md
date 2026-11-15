---
name: systematic-debugging
description: Use when encountering any bug, test failure, or unexpected behavior, before proposing fixes
---

# Systematic Debugging

**Core principle:** find the root cause before attempting a fix. A fix that
addresses the symptom leaves the cause in place to resurface elsewhere.

Complete each phase before moving to the next. The phases matter most under the
conditions that make skipping them tempting — time pressure, an obvious-looking
one-line fix, or a previous fix that didn't hold. Systematic investigation is
faster than guess-and-check thrashing, not slower.

## Phase 1: Root cause investigation

1. **Read the error carefully.** Full stack trace, line numbers, file paths,
   error codes. The exact solution is often already in the message.
2. **Reproduce consistently.** What are the exact steps? Does it happen every
   time? If it isn't reproducible, gather more data rather than guessing.
3. **Check recent changes.** Git diff, recent commits, new dependencies, config
   changes, environment differences.
4. **Gather evidence across component boundaries.** When the system has
   multiple components (CI → build → signing, API → service → database), add
   diagnostic instrumentation at each boundary before proposing anything: log
   what enters, log what exits, verify config propagation. Run once to find
   *where* it breaks, then investigate that component.

   ```bash
   # Layer 1: Workflow
   echo "=== Secrets available in workflow: ==="
   echo "IDENTITY: ${IDENTITY:+SET}${IDENTITY:-UNSET}"

   # Layer 2: Build script
   echo "=== Env vars in build script: ==="
   env | grep IDENTITY || echo "IDENTITY not in environment"

   # Layer 3: Signing script
   echo "=== Keychain state: ==="
   security list-keychains
   security find-identity -v

   # Layer 4: Actual signing
   codesign --sign "$IDENTITY" --verbose=4 "$APP"
   ```

   This tells you which layer fails (secrets → workflow ✓, workflow → build ✗).

5. **Trace the data flow backward.** Where does the bad value originate? What
   called this with it? Keep tracing up to the source and fix there. See
   `root-cause-tracing.md` in this directory for the full technique.

## Phase 2: Pattern analysis

Find similar working code in the same codebase and list every difference
between it and the broken path, however small — "that can't matter" is where
root causes hide. If you are implementing against a reference implementation,
read it completely rather than skimming; partial understanding of a pattern
reliably produces bugs. Check what the code depends on: other components,
settings, environment, and the assumptions it makes about each.

## Phase 3: Hypothesis and testing

State one hypothesis explicitly: "I think X is the root cause because Y." Test
it with the smallest possible change, one variable at a time. If it doesn't
hold, form a new hypothesis rather than stacking another fix on top.

When you don't understand something, say so and investigate it. Don't paper
over the gap.

## Phase 4: Implementation

1. **Write a failing test first** — the simplest reproduction, automated if a
   framework exists, a one-off script otherwise. Use `dev-workflow:test-driven-development`.
2. **Implement one fix** addressing the root cause. No bundled refactoring, no
   "while I'm here" improvements.
3. **Verify** the test passes, no other tests broke, and the original issue is
   actually resolved. Use `dev-workflow:verification-before-completion` before claiming success.

### When the fix doesn't work

Count your attempts. Under three, return to Phase 1 and re-analyze with what
you just learned.

At three or more failed fixes, stop fixing and question the architecture. The
signal is that each fix reveals a new problem somewhere else, each fix demands
"massive refactoring" to implement, or each fix creates fresh symptoms. That
pattern is not a failed hypothesis — it means the design is wrong. Ask whether
the pattern is fundamentally sound or is being kept through inertia, and
discuss with your human partner before attempting another fix.

## Signals you are off track

These redirections from your human partner mean the approach isn't working —
return to Phase 1:

- "Is that not happening?" — you assumed something without verifying it
- "Will it show us…?" — you should have added evidence gathering
- "Stop guessing" — you're proposing fixes without understanding
- "Ultra-think this" — question the fundamentals, not just the symptoms

## When there is genuinely no root cause

Some issues really are environmental, timing-dependent, or external. Once the
investigation is complete: document what you ruled out, implement appropriate
handling (retry, timeout, clear error message), and add logging for next time.
Most "no root cause" conclusions are incomplete investigations, so reach this
one only after Phases 1-3, not instead of them.

## Supporting techniques

- **`root-cause-tracing.md`** — trace bugs backward through the call stack to the original trigger
- **`defense-in-depth.md`** — add validation at multiple layers once the root cause is known
- **`condition-based-waiting.md`** — replace arbitrary timeouts with condition polling
