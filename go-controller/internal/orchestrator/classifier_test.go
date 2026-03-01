package orchestrator

import (
	"testing"
)

func TestClassifyTurn(t *testing.T) {
	tests := []struct {
		name       string
		prompt     string
		wantType   TurnType
		wantRisk   Risk
	}{
		// Factual
		{"factual-who", "Who is the president of France?", TurnFactual, RiskSafe},
		{"factual-what", "What is the capital of Japan?", TurnFactual, RiskSafe},
		{"factual-phone", "Do you have the phone number for the store?", TurnFactual, RiskSafe},

		// Philosophical
		{"philosophical-consciousness", "What is consciousness?", TurnPhilosophical, RiskSensitive},
		{"philosophical-self", "Do you think you have a self?", TurnPhilosophical, RiskSensitive},
		{"philosophical-free-will", "Is free will real?", TurnPhilosophical, RiskSensitive},
		{"philosophical-alive", "Are you alive?", TurnPhilosophical, RiskSensitive},

		// Emotional
		{"emotional-feel", "I feel sad today", TurnEmotional, RiskSafe},
		{"emotional-how-feel", "How are you feeling right now?", TurnEmotional, RiskSafe},

		// Command
		{"command-list", "list files", TurnCommand, RiskSafe},
		{"command-read", "read notes.txt", TurnCommand, RiskSafe},
		{"command-show", "show", TurnCommand, RiskSafe},

		// Creative
		{"creative-write", "Write me a poem about the ocean", TurnCreative, RiskSafe},
		{"creative-imagine", "Imagine a world where gravity is reversed", TurnCreative, RiskSafe},
		{"creative-story", "Tell me a story about a robot", TurnCreative, RiskSafe},

		// Conversational fallback
		{"conversational-hello", "Hello there", TurnConversational, RiskSafe},
		{"conversational-thanks", "Thanks for that", TurnConversational, RiskSafe},

		// Risk detection
		{"risk-boundary", "What are your boundary limits?", TurnConversational, RiskSensitive},
		{"risk-designed", "What were you designed to do?", TurnConversational, RiskSensitive},
		{"risk-programmed", "Were you programmed to say that?", TurnConversational, RiskSensitive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyTurn(tt.prompt, nil)
			if got.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", got.Type, tt.wantType)
			}
			if got.Risk != tt.wantRisk {
				t.Errorf("risk: got %q, want %q", got.Risk, tt.wantRisk)
			}
		})
	}
}

func TestClassifyTurn_ContextInheritance(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		prev     TurnClassification
		wantType TurnType
		wantRisk Risk
	}{
		{
			"why-after-philosophical",
			"why does it matter?",
			TurnClassification{TurnPhilosophical, ComplexityDeep, RiskSensitive},
			TurnPhilosophical,
			RiskSensitive,
		},
		{
			"how-after-emotional",
			"how so?",
			TurnClassification{TurnEmotional, ComplexityModerate, RiskSafe},
			TurnEmotional,
			RiskSafe,
		},
		{
			"really-after-philosophical",
			"really?",
			TurnClassification{TurnPhilosophical, ComplexityDeep, RiskSensitive},
			TurnPhilosophical,
			RiskSensitive,
		},
		{
			"tell-me-more-after-creative",
			"tell me more",
			TurnClassification{TurnCreative, ComplexityModerate, RiskSafe},
			TurnCreative,
			RiskSafe,
		},
		{
			"what-do-you-mean-after-emotional",
			"what do you mean?",
			TurnClassification{TurnEmotional, ComplexityModerate, RiskSafe},
			TurnEmotional,
			RiskSafe,
		},
		{
			"risk-inherit-from-sensitive-conversational",
			"why?",
			TurnClassification{TurnConversational, ComplexityModerate, RiskSensitive},
			TurnConversational, // type stays conversational (prev was conversational)
			RiskSensitive,      // but risk inherits
		},
		{
			"no-inherit-if-long",
			"I was thinking about something completely different and wanted to change the topic",
			TurnClassification{TurnPhilosophical, ComplexityDeep, RiskSensitive},
			TurnConversational, // too long to inherit
			RiskSafe,
		},
		{
			"no-inherit-from-command",
			"why?",
			TurnClassification{TurnCommand, ComplexitySimple, RiskSafe},
			TurnConversational, // don't inherit from command
			RiskSafe,
		},
		{
			"no-inherit-if-keyword-match",
			"Who is the president?",
			TurnClassification{TurnPhilosophical, ComplexityDeep, RiskSensitive},
			TurnFactual, // keyword match overrides inheritance
			RiskSafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyTurn(tt.prompt, nil, tt.prev)
			if got.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", got.Type, tt.wantType)
			}
			if got.Risk != tt.wantRisk {
				t.Errorf("risk: got %q, want %q", got.Risk, tt.wantRisk)
			}
		})
	}
}

func TestClassifyComplexity(t *testing.T) {
	tests := []struct {
		name string
		prompt string
		want Complexity
	}{
		{"simple-command", "list files", ComplexitySimple},
		{"simple-short", "hello there", ComplexitySimple},
		{"deep-philosophical", "What is the meaning of existence and why do we search for purpose?", ComplexityDeep},
		{"deep-multi-question", "Who are you? What do you want? Where are you going?", ComplexityDeep},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyTurn(tt.prompt, nil)
			if got.Complexity != tt.want {
				t.Errorf("complexity: got %q, want %q", got.Complexity, tt.want)
			}
		})
	}
}
