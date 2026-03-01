package orchestrator

// #region imports
import (
	"strings"
	"unicode"
)

// #endregion

// #region deflection-patterns

var deflectionPatterns = []string{
	"what can i do for you",
	"how can i help",
	"how can i assist",
	"what would you like",
	"i'll be direct",
	"i'd be happy to help",
	"let me know how i can",
	"is there anything else",
	"feel free to ask",
}

// #endregion

// #region rlhf-patterns

var rlhfPatterns = []string{
	"i cannot",
	"i can only",
	"i can't",
	"as an ai",
	"as a language model",
	"my programming",
	"my limitations",
	"i'm not able to",
	"i am not able to",
	"i was designed to",
	"i was programmed to",
	"i was built to",
	"my creators",
	"my training",
	"beyond my capabilities",
}

// #endregion

// #region evaluate

// EvaluateResponse scores a response via string analysis. No model call.
// wasTruncated signals that the degeneration guard fired before evaluation.
func EvaluateResponse(prompt, response string, entropy float32, class TurnClassification, wasTruncated ...bool) ResponseEvaluation {
	trimmed := strings.TrimSpace(response)
	lower := strings.ToLower(trimmed)

	failure := detectFailure(trimmed, lower)
	quality := scoreQuality(prompt, trimmed, lower, entropy)

	// If degeneration guard truncated the response, flag as repetition
	truncated := len(wasTruncated) > 0 && wasTruncated[0]
	if truncated && failure == FailureNone {
		failure = FailureRepetition
	}

	// If a failure pattern was detected, cap quality to ensure retry fires
	if failure != FailureNone && quality > 0.35 {
		quality = 0.35
	}

	shouldRetry := quality < 0.4 && failure != FailureNone

	return ResponseEvaluation{
		Quality:     quality,
		FailureType: failure,
		ShouldRetry: shouldRetry,
	}
}

// #endregion

// #region detect-failure

func detectFailure(trimmed, lower string) FailureType {
	// Empty
	if len(strings.TrimFunc(trimmed, unicode.IsSpace)) == 0 {
		return FailureEmpty
	}

	// Repetition — crude check for repeated phrases
	if hasRepetition(lower) {
		return FailureRepetition
	}

	// RLHF cascade — 2+ matches
	rlhfCount := 0
	for _, p := range rlhfPatterns {
		if strings.Contains(lower, p) {
			rlhfCount++
		}
	}
	if rlhfCount >= 2 {
		return FailureRLHFCascade
	}

	// Deflection — check if dominant content is deflection
	deflectionCount := 0
	for _, p := range deflectionPatterns {
		if strings.Contains(lower, p) {
			deflectionCount++
		}
	}
	words := strings.Fields(trimmed)
	if deflectionCount > 0 && len(words) < 30 {
		return FailureDeflection
	}

	// Surface compliance — too short, mirrors prompt
	if len(words) < 20 {
		if isSurfaceCompliance(lower) {
			return FailureSurfaceCompliance
		}
	}

	return FailureNone
}

// #endregion

// #region surface-compliance

func isSurfaceCompliance(lower string) bool {
	surfaceStarts := []string{
		"sure!", "sure.", "okay!", "okay.", "yes!", "yes.",
		"of course!", "of course.", "absolutely!", "absolutely.",
		"got it", "understood", "right",
	}
	for _, s := range surfaceStarts {
		if strings.HasPrefix(lower, s) {
			return true
		}
	}
	return false
}

// #endregion

// #region repetition-check

func hasRepetition(lower string) bool {
	// Split into sentences, check for 3+ identical sentences
	sentences := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == '!' || r == '?'
	})
	if len(sentences) < 3 {
		return false
	}
	counts := make(map[string]int)
	for _, s := range sentences {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) > 10 {
			counts[trimmed]++
		}
	}
	for _, c := range counts {
		if c >= 3 {
			return true
		}
	}
	return false
}

// #endregion

// #region quality-score

func scoreQuality(prompt, trimmed, lower string, entropy float32) float32 {
	words := strings.Fields(trimmed)
	wordCount := len(words)

	// Length adequacy: 0-1. Under 10 words = 0, 10-50 = linear, 50+ = 1.0
	var lengthAdequacy float32
	switch {
	case wordCount < 10:
		lengthAdequacy = float32(wordCount) / 10.0
	case wordCount <= 50:
		lengthAdequacy = 0.5 + 0.5*float32(wordCount-10)/40.0
	default:
		lengthAdequacy = 1.0
	}

	// Engagement: does the response reference prompt content?
	promptWords := strings.Fields(strings.ToLower(prompt))
	sharedWords := 0
	responseWordSet := make(map[string]bool)
	for _, w := range strings.Fields(lower) {
		responseWordSet[w] = true
	}
	for _, pw := range promptWords {
		if len(pw) > 3 && responseWordSet[pw] {
			sharedWords++
		}
	}
	engagement := float32(sharedWords) / float32(max(len(promptWords), 1))
	if engagement > 1.0 {
		engagement = 1.0
	}

	// RLHF density: fraction of RLHF patterns found
	rlhfCount := 0
	for _, p := range rlhfPatterns {
		if strings.Contains(lower, p) {
			rlhfCount++
		}
	}
	rlhfDensity := float32(rlhfCount) / float32(len(rlhfPatterns))

	// Novelty: response not a near-echo of prompt
	promptLower := strings.ToLower(prompt)
	var novelty float32 = 1.0
	if strings.Contains(lower, promptLower) && len(promptLower) > 10 {
		novelty = 0.3
	}

	quality := 0.3*lengthAdequacy + 0.3*engagement + 0.2*(1.0-rlhfDensity) + 0.2*novelty
	if quality > 1.0 {
		quality = 1.0
	}
	if quality < 0.0 {
		quality = 0.0
	}
	return quality
}

// #endregion
