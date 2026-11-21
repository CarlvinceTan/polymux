package hub

import (
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

var (
	orderClause = regexp.MustCompile(`(?i)\border by [a-z]+(\s+(asc|desc))?`)
	fieldGroup  = regexp.MustCompile(`(?i)\b(from|to|cc|bcc|subject|body)\s*\(([^()]*)\)`)
	plainGroup  = regexp.MustCompile(`\(([^()]*)\)`)
	orWord      = regexp.MustCompile(`(?i)\s+or\s+`)
	queryToken  = regexp.MustCompile(`"([^"]*)"|(\S+)`)
)

type queryWord struct {
	value  string
	quoted bool
}

func tokenise(text string) []queryWord {
	out := []queryWord{}
	for _, match := range queryToken.FindAllStringSubmatchIndex(text, -1) {
		if match[2] >= 0 {
			out = append(out, queryWord{value: text[match[2]:match[3]], quoted: true})
		} else {
			out = append(out, queryWord{value: text[match[4]:match[5]]})
		}
	}
	return out
}

// anyOf folds alternatives into IMAP's two-way OR.
func anyOf(terms []imap.SearchCriteria) imap.SearchCriteria {
	out := terms[0]
	for _, term := range terms[1:] {
		out = imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{out, term}}}
	}
	return out
}

func headerTerm(field, value string) imap.SearchCriteria {
	return imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: field, Value: value}}}
}

// mailDates are what people and bots write. IMAP itself refuses anything but its own form.
var mailDates = []string{"2-Jan-2006", "2006-01-02", "2 Jan 2006", "Jan 2, 2006", "January 2, 2006", time.RFC3339}

func parseMailDate(value string) (time.Time, bool) {
	for _, layout := range mailDates {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

var queryFlags = map[string]struct {
	flag imap.Flag
	not  bool
}{
	"seen": {flag: imap.FlagSeen}, "read": {flag: imap.FlagSeen}, "unseen": {imap.FlagSeen, true}, "unread": {imap.FlagSeen, true},
	"flagged": {flag: imap.FlagFlagged}, "starred": {flag: imap.FlagFlagged}, "answered": {flag: imap.FlagAnswered}, "replied": {flag: imap.FlagAnswered},
	"draft": {flag: imap.FlagDraft}, "deleted": {flag: imap.FlagDeleted},
}

// readTerm turns a field and its value into a search term, or reports that they aren't one.
func readTerm(field, value string) (imap.SearchCriteria, bool) {
	if value == "" {
		return imap.SearchCriteria{}, false
	}
	switch field {
	case "from", "to", "cc", "bcc", "subject":
		return headerTerm(field, value), true
	case "body":
		return imap.SearchCriteria{Body: []string{value}}, true
	case "since", "after":
		if date, ok := parseMailDate(value); ok {
			return imap.SearchCriteria{Since: date}, true
		}
	case "before":
		if date, ok := parseMailDate(value); ok {
			return imap.SearchCriteria{Before: date}, true
		}
	case "on":
		if date, ok := parseMailDate(value); ok {
			return imap.SearchCriteria{Since: date, Before: date.AddDate(0, 0, 1)}, true
		}
	case "flag":
		if flag, ok := queryFlags[strings.ToLower(value)]; ok {
			if flag.not {
				return imap.SearchCriteria{NotFlag: []imap.Flag{flag.flag}}, true
			}
			return imap.SearchCriteria{Flag: []imap.Flag{flag.flag}}, true
		}
	}
	return imap.SearchCriteria{}, false
}

func alternatives(text string) []string {
	out := []string{}
	for _, value := range orWord.Split(text, -1) {
		if value = strings.Trim(strings.TrimSpace(value), `"`); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func anywhere(value string) []imap.SearchCriteria {
	return []imap.SearchCriteria{headerTerm("subject", value), headerTerm("from", value), {Body: []string{value}}}
}

// searchCriteria reads the search box's small language: fields followed by terms, joined by
// or and and, with bracketed alternatives. Anything else searches subject, sender and body,
// and a term the server couldn't use is dropped rather than sent. An empty query needs no search.
func searchCriteria(query string) *imap.SearchCriteria {
	text := strings.TrimSpace(orderClause.ReplaceAllString(query, ""))
	if text == "" {
		return nil
	}
	groups := []imap.SearchCriteria{}
	text = fieldGroup.ReplaceAllStringFunc(text, func(match string) string {
		parts := fieldGroup.FindStringSubmatch(match)
		terms := []imap.SearchCriteria{}
		for _, value := range alternatives(parts[2]) {
			if term, ok := readTerm(strings.ToLower(parts[1]), value); ok {
				terms = append(terms, term)
			}
		}
		if len(terms) > 0 {
			groups = append(groups, anyOf(terms))
		}
		return " "
	})
	text = plainGroup.ReplaceAllStringFunc(text, func(match string) string {
		terms := []imap.SearchCriteria{}
		for _, value := range alternatives(match[1 : len(match)-1]) {
			terms = append(terms, anywhere(value)...)
		}
		if len(terms) > 0 {
			groups = append(groups, anyOf(terms))
		}
		return " "
	})
	text = strings.TrimSpace(text)
	if len(groups) > 0 {
		out := imap.SearchCriteria{}
		if rest := searchCriteria(text); rest != nil {
			out = *rest
		}
		for _, group := range groups {
			out.And(&group)
		}
		return &out
	}
	words := tokenise(text)
	terms := []imap.SearchCriteria{}
	disjunction := false
	for index := 0; index < len(words); index++ {
		word := words[index]
		if word.quoted {
			continue
		}
		field := strings.ToLower(word.value)
		switch field {
		case "or":
			disjunction = true
			continue
		case "and", "not":
			continue
		}
		if index+1 >= len(words) {
			continue
		}
		if term, ok := readTerm(field, words[index+1].value); ok {
			terms = append(terms, term)
			index++
		}
	}
	if len(terms) == 0 {
		out := anyOf(anywhere(text))
		return &out
	}
	if disjunction {
		out := anyOf(terms)
		return &out
	}
	out := imap.SearchCriteria{}
	for index := range terms {
		out.And(&terms[index])
	}
	return &out
}
