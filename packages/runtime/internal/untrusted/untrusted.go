package untrusted

import (
	"regexp"
	"strings"
)

var (
	untrustedTag = regexp.MustCompile(`(?i)untrusted_data`)
	envelope     = regexp.MustCompile(`(?s)\A<untrusted_data source="[^"]*">\n(.*)\n</untrusted_data>\z`)
)

// Unwrap returns the text inside one whole envelope, or the text unchanged.
func Unwrap(text string) string {
	if match := envelope.FindStringSubmatch(text); match != nil {
		return match[1]
	}
	return text
}

// Wrap marks external content as data and defuses embedded envelope tags.
func Wrap(source, text string) string {
	source = strings.NewReplacer(`"`, "", "<", "", ">", "").Replace(untrustedTag.ReplaceAllString(source, "untrusted-data"))
	return `<untrusted_data source="` + source + `">` + "\n" + untrustedTag.ReplaceAllString(text, "untrusted-data") + "\n</untrusted_data>"
}
