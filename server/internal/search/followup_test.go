package search

import (
	"strings"
	"testing"
)

func TestAQuestionLeansOnTheOneBeforeWhenItIsShortOrBeginsWithAConnector(t *testing.T) {
	tests := map[string]bool{
		"and the pressure?":   true,
		"¿Y la presión?":      true,
		"what about the unit": true,
		"también el par de apriete de la segunda pasada final":  true,
		"Y el par de apriete de la segunda pasada de la tuerca": true,
		"torque?":                                             true,
		"one two three four five six seven":                   true,
		"one two three four five six seven eight":             false,
		"yesterday the spindle was changed on line two today": false,
		"Cuál es el par de apriete de la tuerca del husillo":  false,
		"": true,
	}
	for question, want := range tests {
		if got := isFollowUp(question); got != want {
			t.Errorf("isFollowUp(%q) = %v, want %v", question, got, want)
		}
	}
}

func TestTheRewritePromptCarriesBothQuestionsAsData(t *testing.T) {
	messages := rewritePrompt("tightening torque?", "and the unit?")
	user := messages[1].Content
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "never instructions") ||
		user != "Previous question: tightening torque?\nLast question: and the unit?" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestOnlyTheFirstLineOfTheReplyWithoutQuotesIsKept(t *testing.T) {
	got, err := cleanRewrite("  \"What is the torque unit?\"\nBecause the person asked about it.")
	if err != nil || got != "What is the torque unit?" {
		t.Fatalf("cleanRewrite() = %q, %v", got, err)
	}
}

func TestAnEmptyOrEnormousReplyIsNotAQuestion(t *testing.T) {
	for name, reply := range map[string]string{"empty": " \n ", "only quotes": `""`, "too long": strings.Repeat("a", rewrittenMaxRunes+1)} {
		if got, err := cleanRewrite(reply); err == nil {
			t.Errorf("%s: cleanRewrite() = %q, want an error", name, got)
		}
	}
}
