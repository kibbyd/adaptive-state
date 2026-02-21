"""Tests for InferenceService."""

import math

from adaptive_inference.service import InferenceService


def test_format_state_preamble_zeros():
    """State preamble with zero vector should show zero norms."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 128, [])
    assert "[Adaptive State Context]" in preamble
    assert "norm=0.0000" in preamble


def test_format_state_preamble_with_evidence():
    """State preamble should include evidence refs."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 128, ["doc1", "doc2"])
    assert "doc1" in preamble
    assert "doc2" in preamble


def test_format_state_preamble_short_vector():
    """Short vector should return empty preamble."""
    svc = InferenceService()
    preamble = svc._format_state_preamble([0.0] * 10, [])
    assert preamble == ""


def test_format_state_preamble_norms():
    """Non-zero segments should produce non-zero norms."""
    svc = InferenceService()
    vec = [0.0] * 128
    vec[0] = 1.0  # preferences segment
    preamble = svc._format_state_preamble(vec, [])
    assert "preferences: norm=1.0000" in preamble


def test_estimate_entropy_no_eval():
    """No eval_count should return 0."""
    svc = InferenceService()
    assert svc._estimate_entropy({}) == 0.0


def test_estimate_entropy_with_eval():
    """eval_count of 200 should return 2.0."""
    svc = InferenceService()
    assert svc._estimate_entropy({"eval_count": 200}) == 2.0


def test_estimate_entropy_capped():
    """Entropy should be capped at 5.0."""
    svc = InferenceService()
    assert svc._estimate_entropy({"eval_count": 1000}) == 5.0
