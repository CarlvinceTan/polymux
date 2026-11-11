package providers

// Empty tags indicate model metadata or endpoint discovery is required.
// It lists the types PiG doesn't serve; see Builtin for the rest.
var Catalogue = map[string][]string{
	"elevenlabs": {"stt", "tts"},
	"ollama":     {},
	"lmstudio":   {},
	"custom":     {},
}

// Recommended maps preset IDs to default model IDs by capability; discovery presets have none.
var Recommended = map[string]map[string]string{
	"openai": {"text": "gpt-6.1-sol", "vision": "gpt-6.1-sol", "image-gen": "gpt-image-2",
		"stt": "gpt-4o-transcribe", "tts": "gpt-4o-mini-tts", "live": "gpt-realtime", "embed": "text-embedding-3-small"},
	"anthropic": {"text": "claude-opus-4-8", "vision": "claude-opus-4-8"},
	"gemini": {"text": "gemini-3.8-flash", "vision": "gemini-3.8-flash", "image-gen": "gemini-3.1-flash-image",
		"video-gen": "veo-3.1-generate-preview", "stt": "gemini-3.8-flash", "tts": "gemini-3.8-flash-tts",
		"live": "gemini-3.8-live", "embed": "gemini-embedding-001"},
	"xai":        {"text": "grok-4.7", "vision": "grok-4.7", "image-gen": "grok-imagine-image-2.0"},
	"deepseek":   {"text": "deepseek-v4-pro"},
	"elevenlabs": {"stt": "scribe_v3", "tts": "eleven_v4"},
}
