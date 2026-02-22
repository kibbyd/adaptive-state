# TODO — Adaptive State

## Ship (do now)

- [ ] Add repo description: "Adaptive behavior on top of a frozen LLM — no fine-tuning, no weight changes"
- [ ] Verify README/PROJECT_RECORD quick start works from a clean clone
- [ ] Push and make repo public

## Test (ongoing)

- [ ] Run 20+ turn sessions mixing factual, casual, and preference-setting prompts
- [ ] Watch for drift: norm saturation, style lock-in, projection over-influence
- [ ] Log any empty responses or think-only failures that slip through
- [ ] Test with a clean DB (no prior evidence) to verify cold-start behavior

## Qwen3 Improvements

- [ ] Disable reasoning (`/no_think`) for casual prompts — eliminates think-only failures
- [ ] Keep reasoning enabled for factual queries (tool calling benefits from chain-of-thought)
- [ ] Consider two-stage generation: reasoning model → answer model (enterprise pattern)

## Future

- [ ] Multi-user support (separate state vectors per user)
- [ ] REST API layer (FastAPI stubs already in py-inference deps)
- [ ] Larger model testing (architecture is model-agnostic, thresholds may need recalibration)
- [ ] Real user satisfaction signal to replace heuristic compliance scoring
- [ ] Long-running drift observation report with actual data
