"""Tests for InferenceService."""

from adaptive_inference.service import InferenceService


def test_format_state_preamble_base():
    """Preamble should always include base system instruction."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 128, [])
    assert "helpful assistant" in preamble
    assert "s0" not in preamble
    assert "norm" not in preamble


def test_format_state_preamble_with_evidence():
    """Preamble should include evidence as prior context."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 128, ["doc1", "doc2"])
    assert "Prior context:" in preamble
    assert "doc1" in preamble
    assert "doc2" in preamble


def test_format_state_preamble_no_evidence():
    """Preamble without evidence should not include separator."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 128, [])
    assert "---" not in preamble
    assert "Prior context" not in preamble


def test_format_state_preamble_no_norms_injected():
    """Non-zero state vector should NOT leak norms into preamble."""
    svc = InferenceService()
    vec = [1.0] * 128
    preamble = svc._format_state_preamble(vec, [])
    assert "1.0" not in preamble
    assert "norm" not in preamble


def test_estimate_entropy_empty_response():
    """Empty response should return 0."""
    svc = InferenceService()
    assert svc._estimate_entropy({}) == 0.0
    assert svc._estimate_entropy({"response": ""}) == 0.0


def test_estimate_entropy_short_response():
    """Short response (~10 words) should produce low entropy."""
    svc = InferenceService()
    result = {"response": "Hello! How can I assist you today? Let me know."}
    assert svc._estimate_entropy(result) == 10.0 / 400.0  # 0.025


def test_estimate_entropy_medium_response():
    """200-word response should return 0.5."""
    svc = InferenceService()
    result = {"response": " ".join(["word"] * 200)}
    assert svc._estimate_entropy(result) == 0.5


def test_estimate_entropy_capped():
    """Entropy should be capped at 1.0 for long responses."""
    svc = InferenceService()
    result = {"response": " ".join(["word"] * 1000)}
    assert svc._estimate_entropy(result) == 1.0


def test_estimate_entropy_strips_thinking():
    """Thinking tokens should be excluded from entropy calculation."""
    svc = InferenceService()
    think_block = "<think>" + " ".join(["reason"] * 500) + "</think>"
    result = {"response": think_block + " Hello there friend."}
    assert svc._estimate_entropy(result) == 3.0 / 400.0  # only 3 visible words
