---
name: assertion style preference
description: What makes a good vs bad assertion candidate
type: feedback
---

Only add assertions that encode real logic — properties that emerge non-trivially from the function's logic across multiple branches or operations.

**Why:** Asserting "I just set this to non-nil, therefore it is non-nil" is weak glue-code assertion with no value.

**How to apply:** A good assertion checks a property whose truth requires understanding how the branches/operations combine — e.g. a length invariant, a structural relationship between two values, a state-machine postcondition, or a semantic contract at a function boundary (like "result is always absolute" in ToAbsPath, which requires reasoning about all 3 branches). Bad assertions just re-state what the immediately preceding line already made obvious.
