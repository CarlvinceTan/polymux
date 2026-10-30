package store

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Unicode 17 Emoji property, merged from unicode.org/Public/17.0.0/ucd/emoji/emoji-data.txt.
// Copyright © 2025 Unicode, Inc. See Emoji-LICENSE.txt.
var emojiBase = regexp.MustCompile(`^[` +
	`\x{23}\x{2a}\x{30}-\x{39}\x{a9}\x{ae}\x{203c}\x{2049}\x{2122}\x{2139}` +
	`\x{2194}-\x{2199}\x{21a9}-\x{21aa}\x{231a}-\x{231b}\x{2328}\x{23cf}` +
	`\x{23e9}-\x{23f3}\x{23f8}-\x{23fa}\x{24c2}\x{25aa}-\x{25ab}\x{25b6}\x{25c0}` +
	`\x{25fb}-\x{25fe}\x{2600}-\x{2604}\x{260e}\x{2611}\x{2614}-\x{2615}\x{2618}\x{261d}` +
	`\x{2620}\x{2622}-\x{2623}\x{2626}\x{262a}\x{262e}-\x{262f}\x{2638}-\x{263a}` +
	`\x{2640}\x{2642}\x{2648}-\x{2653}\x{265f}-\x{2660}\x{2663}\x{2665}-\x{2666}\x{2668}` +
	`\x{267b}\x{267e}-\x{267f}\x{2692}-\x{2697}\x{2699}\x{269b}-\x{269c}\x{26a0}-\x{26a1}` +
	`\x{26a7}\x{26aa}-\x{26ab}\x{26b0}-\x{26b1}\x{26bd}-\x{26be}\x{26c4}-\x{26c5}\x{26c8}` +
	`\x{26ce}-\x{26cf}\x{26d1}\x{26d3}-\x{26d4}\x{26e9}-\x{26ea}\x{26f0}-\x{26f5}` +
	`\x{26f7}-\x{26fa}\x{26fd}\x{2702}\x{2705}\x{2708}-\x{270d}\x{270f}\x{2712}\x{2714}` +
	`\x{2716}\x{271d}\x{2721}\x{2728}\x{2733}-\x{2734}\x{2744}\x{2747}\x{274c}\x{274e}` +
	`\x{2753}-\x{2755}\x{2757}\x{2763}-\x{2764}\x{2795}-\x{2797}\x{27a1}\x{27b0}\x{27bf}` +
	`\x{2934}-\x{2935}\x{2b05}-\x{2b07}\x{2b1b}-\x{2b1c}\x{2b50}\x{2b55}\x{3030}\x{303d}` +
	`\x{3297}\x{3299}\x{1f004}\x{1f0cf}\x{1f170}-\x{1f171}\x{1f17e}-\x{1f17f}\x{1f18e}` +
	`\x{1f191}-\x{1f19a}\x{1f1e6}-\x{1f1ff}\x{1f201}-\x{1f202}\x{1f21a}\x{1f22f}` +
	`\x{1f232}-\x{1f23a}\x{1f250}-\x{1f251}\x{1f300}-\x{1f321}\x{1f324}-\x{1f393}` +
	`\x{1f396}-\x{1f397}\x{1f399}-\x{1f39b}\x{1f39e}-\x{1f3f0}\x{1f3f3}-\x{1f3f5}` +
	`\x{1f3f7}-\x{1f4fd}\x{1f4ff}-\x{1f53d}\x{1f549}-\x{1f54e}\x{1f550}-\x{1f567}` +
	`\x{1f56f}-\x{1f570}\x{1f573}-\x{1f57a}\x{1f587}\x{1f58a}-\x{1f58d}\x{1f590}` +
	`\x{1f595}-\x{1f596}\x{1f5a4}-\x{1f5a5}\x{1f5a8}\x{1f5b1}-\x{1f5b2}\x{1f5bc}` +
	`\x{1f5c2}-\x{1f5c4}\x{1f5d1}-\x{1f5d3}\x{1f5dc}-\x{1f5de}\x{1f5e1}\x{1f5e3}` +
	`\x{1f5e8}\x{1f5ef}\x{1f5f3}\x{1f5fa}-\x{1f64f}\x{1f680}-\x{1f6c5}\x{1f6cb}-\x{1f6d2}` +
	`\x{1f6d5}-\x{1f6d8}\x{1f6dc}-\x{1f6e5}\x{1f6e9}\x{1f6eb}-\x{1f6ec}\x{1f6f0}` +
	`\x{1f6f3}-\x{1f6fc}\x{1f7e0}-\x{1f7eb}\x{1f7f0}\x{1f90c}-\x{1f93a}\x{1f93c}-\x{1f945}` +
	`\x{1f947}-\x{1f9ff}\x{1fa70}-\x{1fa7c}\x{1fa80}-\x{1fa8a}\x{1fa8e}-\x{1fac6}\x{1fac8}` +
	`\x{1facd}-\x{1fadc}\x{1fadf}-\x{1faea}\x{1faef}-\x{1faf8}` + `]$`)

func validReaction(emoji string) bool {
	if len(emoji) == 0 || len(emoji) > 128 || !utf8.ValidString(emoji) {
		return false
	}
	clusters := graphemes.FromString(emoji)
	if !clusters.Next() || clusters.Next() {
		return false
	}
	first, _ := utf8.DecodeRuneInString(emoji)
	if !emojiBase.MatchString(string(first)) {
		return false
	}
	if first < 128 && !strings.ContainsRune(emoji, '\u20e3') {
		return false
	}
	for _, r := range emoji {
		modifier := r == '\u200d' || r == '\u20e3' || r == '\ufe0f' || r == '\ufe0e'
		tag := r >= 0xe0020 && r <= 0xe007f
		if !modifier && !tag && !emojiBase.MatchString(string(r)) {
			return false
		}
	}
	return true
}
