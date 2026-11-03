package main

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// commands splits a shell line into the simple commands it would run, however they're nested.
// Anything only running it could settle marks the result opaque: a name that isn't literal,
// a redirect to a file, or an assignment, which can change what a later command does.
func commands(line string) subject {
	out := subject{kind: KindCommand}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(line), "")
	if err != nil {
		out.texts, out.opaque = []string{line}, true
		return out
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			out.opaque = out.opaque || len(node.Assigns) > 0
			if len(node.Args) == 0 {
				break
			}
			text, name, known := render(node.Args)
			out.opaque = out.opaque || !known
			out.texts = append(out.texts, text)
			if base := filepath.Base(name); base != name {
				out.aliases = append(out.aliases, base+strings.TrimPrefix(text, name))
			}
		case *syntax.Redirect:
			out.opaque = out.opaque || !harmless(node)
		case *syntax.FuncDecl, *syntax.DeclClause, *syntax.CoprocClause:
			out.opaque = true
		}
		return true
	})
	return out
}

// render joins a command's words. Known is false when its name has to be worked out by the shell.
func render(args []*syntax.Word) (text, name string, known bool) {
	words := make([]string, len(args))
	for at, arg := range args {
		value, ok := literal(arg)
		if !ok {
			value = "$?"
		}
		words[at] = value
	}
	name = words[0]
	_, known = literal(args[0])
	return strings.Join(words, " "), name, known && !strings.ContainsAny(name, "*?[{~")
}

// literal gives a word's value when nothing in it needs expanding.
func literal(word *syntax.Word) (string, bool) {
	var out strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			out.WriteString(unescape(part.Value))
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			out.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				text, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				out.WriteString(text.Value)
			}
		default:
			return "", false
		}
	}
	return out.String(), true
}

func unescape(text string) string {
	var out strings.Builder
	for at := 0; at < len(text); at++ {
		if text[at] == '\\' && at+1 < len(text) {
			at++
		}
		out.WriteByte(text[at])
	}
	return out.String()
}

// harmless allows here-documents, joined streams and /dev/null, which reach no file.
func harmless(redirect *syntax.Redirect) bool {
	if redirect.Op == syntax.Hdoc || redirect.Op == syntax.DashHdoc || redirect.Op == syntax.WordHdoc {
		return true
	}
	target, ok := literal(redirect.Word)
	if redirect.Op == syntax.DplIn || redirect.Op == syntax.DplOut {
		return ok && strings.Trim(target, "0123456789-") == ""
	}
	return ok && target == "/dev/null"
}
