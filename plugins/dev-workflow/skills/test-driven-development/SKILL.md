---
name: test-driven-development
description: Use when implementing any feature or bugfix, before writing implementation code
---

# Test-Driven Development (TDD)

Write the test first. Watch it fail. Write the minimal code to pass.

**Core principle:** if you didn't watch the test fail, you don't know it tests
the right thing.

Test-first and test-after are not the same activity. A test written after the
code is biased by the code you already wrote: it verifies the cases you
remembered rather than the ones you would have discovered, and it passes on its
first run — which proves nothing about whether it can catch the bug. Watching it
fail first is the only evidence that the test has teeth.

## When to Use

Always: new features, bug fixes, refactoring, behavior changes.

Ask your human partner before skipping it for throwaway prototypes, generated
code, or configuration files.

Wrote code before the test? Delete it and implement fresh from the test.
Keeping it as "reference" and adapting it while writing the test is
testing-after with extra steps.

## RED — write a failing test

One behavior, a name that describes that behavior, real code rather than mocks.

<Good>
```typescript
test('retries failed operations 3 times', async () => {
  let attempts = 0;
  const operation = () => {
    attempts++;
    if (attempts < 3) throw new Error('fail');
    return 'success';
  };

  const result = await retryOperation(operation);

  expect(result).toBe('success');
  expect(attempts).toBe(3);
});
```
Clear name, tests real behavior, one thing
</Good>

<Bad>
```typescript
test('retry works', async () => {
  const mock = jest.fn()
    .mockRejectedValueOnce(new Error())
    .mockRejectedValueOnce(new Error())
    .mockResolvedValueOnce('success');
  await retryOperation(mock);
  expect(mock).toHaveBeenCalledTimes(3);
});
```
Vague name, asserts on the mock instead of the code
</Bad>

## Verify RED — watch it fail

Run the test. Confirm it fails rather than errors, that the failure message is
the one you expect, and that it fails because the feature is missing rather
than because of a typo.

Passes already? You are testing existing behavior — fix the test. Errors
instead of failing? Fix the error and re-run until it fails correctly.

## GREEN — minimal code

Write the simplest code that passes the test.

<Good>
```typescript
async function retryOperation<T>(fn: () => Promise<T>): Promise<T> {
  for (let i = 0; i < 3; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i === 2) throw e;
    }
  }
  throw new Error('unreachable');
}
```
</Good>

Don't add options the test doesn't demand, refactor neighbouring code, or
"improve" beyond what makes it pass.

## Verify GREEN — watch it pass

Run the test. Confirm it passes, that other tests still pass, and that the
output is pristine — no stray errors or warnings. If it fails, fix the code,
not the test.

## REFACTOR

Only once green: remove duplication, improve names, extract helpers. Keep the
tests green and don't add behavior. Then write the next failing test.

## Good Tests

| Quality | Good | Bad |
|---------|------|-----|
| **Minimal** | One thing. "and" in the name? Split it. | `test('validates email and domain and whitespace')` |
| **Clear** | Name describes the behavior | `test('test1')` |
| **Shows intent** | Demonstrates the desired API | Obscures what the code should do |

When writing or changing any test, read [writing-good-tests.md](writing-good-tests.md)
for the rules that keep tests honest:

- Name the production change that would make the test fail — before writing it
- Assert on real behavior, never on mock behavior
- Keep test-only code in test utilities, out of production classes
- Understand a dependency's side effects before mocking it

## When Stuck

| Problem | Solution |
|---------|----------|
| Don't know how to test | Write the wished-for API. Write the assertion first. Ask your human partner. |
| Test too complicated | The design is too complicated. Simplify the interface. |
| Must mock everything | The code is too coupled. Use dependency injection. |
| Test setup huge | Extract helpers. Still complex? Simplify the design. |

## Debugging

Found a bug? Write a failing test that reproduces it, then follow the cycle.
The test proves the fix and prevents the regression.
