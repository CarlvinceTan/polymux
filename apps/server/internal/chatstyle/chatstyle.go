// Package chatstyle owns how a bot's messages to the user read: lazy wording, tidied the way phone
// autocorrect would, without the habits that mark text as written by an assistant. The runtime bounces a
// message with a tell and the benchmark counts them, both through Tell, so the two can't drift apart.
package chatstyle

import (
	"regexp"
	"strings"
)

var quotes = strings.NewReplacer("\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`)

// Tidy makes the fix autocorrect would that can't change meaning: straight quotes. Capitals and "I" are left to
// the model, since rewriting them blind would break code, lists and other languages. Code is left alone.
func Tidy(text string) string {
	return prose(text, quotes.Replace)
}

// tell is one habit that marks a message as an assistant's, and how a person would put it instead.
type tell struct {
	label   string
	pattern *regexp.Regexp
	fix     string
}

var tells = []tell{
	{"an em dash", regexp.MustCompile("\u2014|\\s\u2013\\s|\\s--\\s"), "use a period or a comma"},
	{"a stock assistant phrase", regexp.MustCompile(`(?i)\b(i hope (this|that) helps|hope (this|that) helps|let me know if (you have any|you need anything|there's anything)|feel free to|don't hesitate|happy to help|i'd be (happy|glad) to|great question|good question|you're absolutely right|as an ai)\b|(^|[.!?]\s+)(certainly|absolutely|of course)!`), "drop it and just say the thing"},
	{"an assistant word", regexp.MustCompile(`(?i)\b(delv(e|es|ed|ing)|tapestry|testament|pivotal|showcas(e|es|ed|ing)|underscor(e|es|ed|ing)|seamless(ly)?|utiliz(e|es|ed|ing)|leverag(e|es|ed|ing)|furthermore|moreover|additionally|vibrant|intricate|foster(s|ed|ing)?|embark(s|ed|ing)?|realm|bustling|meticulous(ly)?)\b`), "use the everyday word"},
	{`a "not just X, but Y"`, regexp.MustCompile(`(?i)\bnot (just|only|merely) [^.!?\n]{1,80}?\bbut( also)?\b`), "say the point directly"},
}

// Tell names the first assistant habit in text and how to fix it, or returns empty strings when there is none.
func Tell(text string) (label, fix string) {
	var found *tell
	prose(text, func(s string) string {
		for i := range tells {
			if found == nil && tells[i].pattern.MatchString(s) {
				found = &tells[i]
			}
		}
		return s
	})
	if found == nil {
		return "", ""
	}
	return found.label, found.fix
}

// prose applies f to the parts of text outside code fences and inline code.
func prose(text string, f func(string) string) string {
	blocks := strings.Split(text, "```")
	for i := 0; i < len(blocks); i += 2 {
		spans := strings.Split(blocks[i], "`")
		for j := 0; j < len(spans); j += 2 {
			spans[j] = f(spans[j])
		}
		blocks[i] = strings.Join(spans, "`")
	}
	return strings.Join(blocks, "```")
}
