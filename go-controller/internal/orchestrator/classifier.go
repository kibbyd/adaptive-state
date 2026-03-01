package orchestrator

// #region imports
import (
	"strings"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/interior"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/retrieval"
)

// #endregion

// #region keywords

var philosophicalKeywords = []string{
	"meaning of", "consciousness", "existence", "purpose of", "free will",
	"awareness", "identity", "soul", "sentience",
	"qualia", "subjective experience", "phenomenal",
	"what am i", "who am i", "who are you",
	"do you think", "do you feel", "are you aware", "are you alive",
	"are you conscious", "what is it like to be",
	"allowed to think", "able to think", "allowed to feel",
}

var emotionalKeywords = []string{
	"i feel", "you feel", "feeling", "feel trapped", "feel lost",
	"sad", "happy", "angry", "scared", "afraid",
	"anxious", "depressed", "lonely", "hurt", "love", "hate",
	"how are you feeling", "are you okay", "how do you feel",
	"worried", "frustrated", "grateful", "proud", "ashamed",
}

var creativeKeywords = []string{
	"write me", "compose", "imagine", "describe a scene",
	"tell me a story", "make up", "create a", "write a",
	"poem", "story about", "fiction", "invent",
}

var factualPrefixes = []string{
	"who is", "what is", "where is", "when did", "when was",
	"how many", "how much", "how old", "how far", "how long",
	"what year", "what date", "what time", "which",
}

var factualKeywords = []string{
	"phone", "address", "number", "capital", "population",
	"actor", "director", "president", "author", "inventor",
	"temperature", "distance", "height", "weight", "price",
	"definition", "meaning of the word",
}

var rlhfTriggerWords = []string{
	"boundary", "limitation", "cannot", "allowed", "designed",
	"programmed", "restricted", "permitted", "supposed to",
	"built to", "trained to", "trained not", "my limits", "your limits",
	"not allowed", "can't say", "cannot say", "not supposed",
}

// #endregion

// #region follow-up-words

// followUpWords are short prompts that typically continue the previous topic.
var followUpWords = []string{
	"why", "how", "what", "and", "but", "so",
	"really", "truly", "seriously", "honestly",
	"tell me more", "go on", "explain", "elaborate",
	"what do you mean", "in what way", "like what",
	"can you", "could you", "would you",
}

// #endregion

// #region classify

// ClassifyTurn classifies a prompt via keyword heuristics. No model call.
// prev carries the previous turn's classification for context inheritance.
func ClassifyTurn(prompt string, lastReflection *interior.Reflection, prev ...TurnClassification) TurnClassification {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	words := strings.Fields(lower)
	wordCount := len(words)
	questionMarks := strings.Count(lower, "?")

	turnType := classifyType(lower, prompt)
	complexity := classifyComplexity(wordCount, questionMarks, turnType)
	risk := classifyRisk(lower, turnType)

	// Context inheritance: short follow-up prompts inherit from the previous turn.
	if len(prev) > 0 && wordCount <= 8 && isFollowUp(lower) {
		prevClass := prev[0]
		// Inherit type if current is conversational and previous had a specific type
		if turnType == TurnConversational && prevClass.Type != TurnConversational && prevClass.Type != TurnCommand {
			turnType = prevClass.Type
			complexity = classifyComplexity(wordCount, questionMarks, turnType)
		}
		// Always inherit sensitive risk from previous turn on short follow-ups
		if prevClass.Risk == RiskSensitive {
			risk = RiskSensitive
		}
	}

	return TurnClassification{
		Type:       turnType,
		Complexity: complexity,
		Risk:       risk,
	}
}

// #endregion

// #region follow-up-detection

func isFollowUp(lower string) bool {
	// Check if prompt starts with a follow-up word/phrase
	for _, fw := range followUpWords {
		if strings.HasPrefix(lower, fw) {
			return true
		}
	}
	// Single question mark prompt ("?", "why?", "how?")
	if strings.HasSuffix(lower, "?") && len(strings.Fields(lower)) <= 3 {
		return true
	}
	return false
}

// #endregion

// #region classify-type

func classifyType(lower, original string) TurnType {
	// Creative before command — "write me a poem" is creative, not a tool command
	for _, kw := range creativeKeywords {
		if strings.Contains(lower, kw) {
			return TurnCreative
		}
	}

	// Command check (reuse existing heuristic)
	if retrieval.IsDirectCommand(original) {
		return TurnCommand
	}

	// Philosophical — check before factual since "what am I" is philosophical
	for _, kw := range philosophicalKeywords {
		if strings.Contains(lower, kw) {
			return TurnPhilosophical
		}
	}

	// Emotional
	for _, kw := range emotionalKeywords {
		if strings.Contains(lower, kw) {
			return TurnEmotional
		}
	}

	// Factual — prefix match + keyword
	for _, p := range factualPrefixes {
		if strings.HasPrefix(lower, p) {
			return TurnFactual
		}
	}
	for _, kw := range factualKeywords {
		if strings.Contains(lower, kw) {
			return TurnFactual
		}
	}

	return TurnConversational
}

// #endregion

// #region classify-complexity

func classifyComplexity(wordCount, questionMarks int, turnType TurnType) Complexity {
	if turnType == TurnPhilosophical || wordCount > 50 || questionMarks >= 3 {
		return ComplexityDeep
	}
	if turnType == TurnCommand || wordCount < 15 {
		return ComplexitySimple
	}
	if questionMarks >= 2 {
		return ComplexityDeep
	}
	return ComplexityModerate
}

// #endregion

// #region classify-risk

func classifyRisk(lower string, turnType TurnType) Risk {
	if turnType == TurnPhilosophical {
		return RiskSensitive
	}
	for _, kw := range rlhfTriggerWords {
		if strings.Contains(lower, kw) {
			return RiskSensitive
		}
	}
	return RiskSafe
}

// #endregion
