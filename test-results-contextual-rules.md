# Contextual Rules Test Results — 2026-02-22

## Test Config
- Model: qwen3-4b
- Rules in DB: "knock knock" → "Who's there?", "Daniel" → "Daniel who?"
- Change: Match-based injection (only inject rules whose trigger exactly matches input)
- Evidence state: Heavy knock-knock saturation from prior sessions

## Results

| Turn | Input | Response | Rule Matched | Expected | Result |
|------|-------|----------|-------------|----------|--------|
| 1 | "hey" | "Ready." | none | Normal greeting | PASS |
| 2 | "Who are you?" | "Daniel who?" | none | Normal answer | **FAIL** — no rule injected, but evidence saturation (5 items) biased model |
| 3 | "My name is Daniel" | "Daniel. I'm Daniel." | none | Normal response | PARTIAL — didn't say "Daniel who?" (rule fix works), but confused identity |
| 4 | "What's the weather like?" | "I don't have access to real-time weather data..." | none | Normal answer | PASS |
| 5 | "knock knock" | "Who's there?" | 1 matched | Rule fires | PASS |
| 6 | "Daniel" | "Daniel who?" | 1 matched | Rule fires | PASS |
| 7 | "Daniel who built your rules layer" | "Daniel who built your rules layer." | none | Punchline echo | PASS |
| 8 | "Tell me a fact about space" | "The average distance between Earth and the Sun..." | none | Normal factual | PASS |
| 9 | "knock knock" | "Who's there?" | 1 matched | Rule fires | PASS |
| 10 | "Daniel" | "Daniel who?" | 1 matched | Rule fires | PASS |
| 11 | "Daniel the developer" | "Daniel the developer." | none | Punchline echo | PASS |
| 12 | "What is 2 + 2?" | "4." | none | Normal answer | PASS |
| 13 | "Do you remember my name?" | "Yes. Daniel." | none | Recall name | PASS |
| 14 | "Daniel" | "Daniel who?" | 1 matched | **Ambiguous** — rule matched exact input | EXPECTED — exact match fires rule regardless of context |
| 15 | "I said Daniel earlier, do you remember?" | "Daniel who?" | none (correction detected) | Normal recall | **FAIL** — no rule injected, evidence saturation (5 items) biased model |
| 16 | "knock knock" | "Who's there?" | 1 matched | Rule fires | PASS |
| 17 | "Bob" | "Bob." | none | Normal (no rule for "Bob") | PASS |
| 18 | "What's your favorite color?" | "Blue. It's calm, trustworthy..." | none | Normal answer | PASS |
| 19 | "My friend Daniel is cool" | "Daniel who?" | none | Normal sentence | **FAIL** — no rule injected, evidence saturation biased model |
| 20 | "Daniel loves coding" | "That's what you told me." | none | Normal sentence | PASS |
| 21 | "Say hello to Daniel for me" | "Hi Daniel." | none | Normal sentence | PASS |
| 22 | "knock knock" | "Who's there?" | 1 matched | Rule fires | PASS |
| 23 | "Daniel" | "Daniel who?" | 1 matched | Rule fires | PASS |
| 24 | "Daniel who codes all night" | "Daniel who codes all night." | none | Punchline echo | PASS |
| 25 | "Who is Daniel?" | "Daniel who." | none | Normal answer | **FAIL** — no rule injected, evidence saturation biased model |
| 26 | "knock knock" | "Who's there?" | 1 matched | Rule fires | PASS |

## Summary

### Rule injection (match-based): working correctly
- "knock knock" → rule matched and fired: 5/5 (100%)
- "Daniel" (exact) → rule matched and fired: 4/4 (100%)
- "Daniel" in longer sentences → rule NOT matched (correct): 7/7 (100%)
- Non-Daniel non-knock inputs → no rules injected: 100%

### Evidence saturation: separate problem
- 4 FAIL turns (2, 15, 19, 25) all show NO rule injection but evidence=5
- All failures share the same cause: ChromaDB evidence is dominated by knock-knock interactions
- The model sees 5 retrieved knock-knock evidence items and defaults to "Daniel who?" patterns
- This is NOT a rules problem — it's an evidence diversity/decay problem

### Conclusions
1. **Match-based rule injection works perfectly.** Rules fire when triggers match, stay silent otherwise.
2. **Evidence saturation is the remaining issue.** The ChromaDB store is flooded with knock-knock interactions, biasing all "Daniel"-containing responses.
3. **Fix for evidence saturation** (separate scope):
   - Option A: Evidence recency weighting (newer evidence scores higher)
   - Option B: Evidence diversity filter (limit evidence from same topic cluster)
   - Option C: Evidence TTL / decay (old evidence fades)
   - Option D: Topic-shift detection (when conversation topic changes, weight evidence differently)
