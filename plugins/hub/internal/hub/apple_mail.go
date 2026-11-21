package hub

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

const appleMailAccount = "apple-mail"

var (
	appleField   = regexp.MustCompile(`(?i)\b(?:since|after|before)\s+\S+`)
	appleWord    = regexp.MustCompile(`[\pL\pN@._-]{2,}`)
	appleSince   = regexp.MustCompile(`(?i)\b(?:since|after)\s+(\d{1,2}-[A-Za-z]{3}-\d{4}|\d{4}-\d{2}-\d{2})`)
	appleFields  = regexp.MustCompile(`(?i)\b(?:subject|from|to|body)\b`)
	appleOr      = regexp.MustCompile(`(?i)\s+OR\s+`)
	appleIgnored = []string{"since", "after", "before", "subject", "from", "to", "body", "or", "and"}
)

// appleMailIndex finds Mail's envelope database, newest version first.
func appleMailIndex(home string) string {
	root := filepath.Join(home, "Library", "Mail")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	versions := []int{}
	for _, entry := range entries {
		if number, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "V")); err == nil && strings.HasPrefix(entry.Name(), "V") {
			versions = append(versions, number)
		}
	}
	slices.Sort(versions)
	slices.Reverse(versions)
	for _, version := range versions {
		path := filepath.Join(root, "V"+strconv.Itoa(version), "MailData", "Envelope Index")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func appleTerms(queries []string) []string {
	out := []string{}
	for _, query := range queries {
		for _, term := range appleWord.FindAllString(appleField.ReplaceAllString(query, " "), -1) {
			if !slices.Contains(appleIgnored, strings.ToLower(term)) && !slices.Contains(out, term) {
				out = append(out, term)
			}
		}
	}
	return out[:min(len(out), 8)]
}

func appleCutoff(queries []string, now time.Time) time.Time {
	earliest := now.AddDate(0, 0, -31)
	for _, query := range queries {
		if match := appleSince.FindStringSubmatch(query); match != nil {
			if date, ok := parseMailDate(match[1]); ok && date.Before(earliest) {
				earliest = date
			}
		}
	}
	return earliest
}

// appleMatches holds a row to what a query asked, since the SQL only finds candidates.
func appleMatches(query, haystack string) bool {
	cleaned := appleFields.ReplaceAllString(appleField.ReplaceAllString(query, " "), " ")
	cleaned = strings.NewReplacer("(", " ", ")", " ", `"`, " ").Replace(cleaned)
	for _, alternative := range appleOr.Split(cleaned, -1) {
		terms := appleWord.FindAllString(strings.ToLower(alternative), -1)
		if len(terms) > 0 && !slices.ContainsFunc(terms, func(term string) bool { return !strings.Contains(haystack, term) }) {
			return true
		}
	}
	return false
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
}

// appleMailSearch reads Mail's index directly and read-only, which covers accounts only Mail can
// open, such as a university's. It neither launches nor controls Mail, and needs Full Disk Access.
func appleMailSearch(ctx context.Context, input toolSearch) searchResult {
	out := searchResult{Messages: []toolEnvelope{}, Errors: []searchIssue{}}
	home, err := os.UserHomeDir()
	if runtime.GOOS != "darwin" || err != nil {
		return out
	}
	return appleMailQuery(ctx, appleMailIndex(home), input, time.Now())
}

func openAppleIndex(ctx context.Context, path, options string) (*sql.DB, error) {
	address := url.URL{Scheme: "file", Path: path, RawQuery: options}
	database, err := sql.Open("sqlite", address.String())
	if err != nil {
		return nil, err
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Err(); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func appleMailQuery(ctx context.Context, path string, input toolSearch, now time.Time) searchResult {
	out := searchResult{Messages: []toolEnvelope{}, Errors: []searchIssue{}}
	terms := appleTerms(input.Queries)
	if path == "" || len(terms) == 0 {
		return out
	}
	// Plain read-only sees what Mail has yet to checkpoint. A database with no -shm needs immutable to open at all.
	database, err := openAppleIndex(ctx, path, "mode=ro")
	if err != nil {
		database, err = openAppleIndex(ctx, path, "mode=ro&immutable=1")
	}
	if err != nil {
		return out
	}
	defer database.Close()
	predicates := make([]string, 0, len(terms))
	arguments := []any{appleCutoff(input.Queries, now).Unix()}
	for _, term := range terms {
		predicates = append(predicates, `(lower(s.subject) LIKE ? ESCAPE '\' OR lower(a.address) LIKE ? ESCAPE '\' OR lower(a.comment) LIKE ? ESCAPE '\' OR lower(z.summary) LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(strings.ToLower(term)) + "%"
		arguments = append(arguments, like, like, like, like)
	}
	limit := max(input.MaxResults, 1)
	arguments = append(arguments, max(40, min(limit*10, 120)))
	rows, err := database.QueryContext(ctx, `SELECT m.ROWID, COALESCE(m.document_id, ''), COALESCE(s.subject, ''), COALESCE(a.address, ''), COALESCE(a.comment, ''),
		COALESCE(z.summary, ''), COALESCE(m.date_received, 0)
		FROM messages m JOIN subjects s ON s.ROWID = m.subject LEFT JOIN addresses a ON a.ROWID = m.sender
		LEFT JOIN summaries z ON z.ROWID = m.summary JOIN mailboxes mb ON mb.ROWID = m.mailbox
		WHERE m.deleted = 0 AND m.date_received >= ? AND lower(mb.url) LIKE '%inbox%' AND (`+strings.Join(predicates, " OR ")+`)
		ORDER BY m.date_received DESC LIMIT ?`, arguments...)
	if err != nil {
		for _, query := range input.Queries {
			out.Errors = append(out.Errors, searchIssue{appleMailAccount, "Apple Mail", query, "Apple Mail couldn't be read."})
		}
		return out
	}
	defer rows.Close()
	for rows.Next() && len(out.Messages) < min(limit, toolSearchMax) {
		var (
			rowid, received                   int64
			document, subject, sender, person string
			summary                           string
		)
		if rows.Scan(&rowid, &document, &subject, &sender, &person, &summary, &received) != nil {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{subject, sender, person, summary}, "\n"))
		queries := []int{}
		for index, query := range input.Queries {
			if appleMatches(query, haystack) {
				queries = append(queries, index)
			}
		}
		if len(queries) == 0 {
			continue
		}
		if document == "" {
			document = "apple-mail:" + strconv.FormatInt(rowid, 10)
		}
		preview := []rune(strings.Join(strings.FieldsFunc(summary, unicode.IsSpace), " "))
		out.Messages = append(out.Messages, toolEnvelope{Account: appleMailAccount, Email: "Apple Mail", ID: document, Subject: subject, From: person, Address: sender,
			Date: time.Unix(received, 0).UTC().Format(time.RFC3339), Preview: string(preview[:min(len(preview), 240)]), at: received * 1000, queries: queries})
	}
	return out
}
