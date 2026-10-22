package untrusted

import (
	"regexp"
	"strings"
)

var untrustedTag = regexp.MustCompile(`(?i)untrusted_data`)

// Wrap marks external content as data and defuses embedded envelope tags.
func Wrap(source, text string) string {
	source = strings.NewReplacer(`"`, "", "<", "", ">", "").Replace(untrustedTag.ReplaceAllString(source, "untrusted-data"))
	return `<untrusted_data source="` + source + `">` + "\n" + untrustedTag.ReplaceAllString(text, "untrusted-data") + "\n</untrusted_data>"
}
