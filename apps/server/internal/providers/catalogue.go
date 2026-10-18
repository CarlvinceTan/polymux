package providers

// Empty tags indicate model metadata or endpoint discovery is required.
var Catalogue = map[string][]string{
	"openai":     {"text", "vision", "image-gen", "stt", "tts", "live", "embed"},
	"anthropic":  {"text", "vision"},
	"gemini":     {"text", "vision", "image-gen", "video-gen", "stt", "tts", "live", "embed"},
	"openrouter": {},
	"xai":        {"text", "vision", "image-gen"},
	"deepseek":   {"text"},
	"elevenlabs": {"stt", "tts"},
	"ollama":     {},
	"lmstudio":   {},
	"custom":     {},
}

// Recommended maps preset IDs to default model IDs by capability; discovery presets have none.
var Recommended = map[string]map[string]string{
	"openai": {"text": "gpt-4.1", "vision": "gpt-4.1", "image-gen": "gpt-image-1",
		"stt": "gpt-4o-transcribe", "tts": "gpt-4o-mini-tts", "live": "gpt-realtime", "embed": "text-embedding-3-small"},
	"anthropic": {"text": "claude-sonnet-4-5", "vision": "claude-sonnet-4-5"},
	"gemini": {"text": "gemini-3.8-flash", "vision": "gemini-3.8-flash", "image-gen": "gemini-2.5-flash-image",
		"video-gen": "veo-3.0-generate-preview", "stt": "gemini-3.8-flash", "tts": "gemini-2.5-flash-preview-tts",
		"live": "gemini-3.8-live", "embed": "gemini-embedding-001"},
	"xai":        {"text": "grok-4", "vision": "grok-4", "image-gen": "grok-2-image"},
	"deepseek":   {"text": "deepseek-chat"},
	"elevenlabs": {"stt": "scribe_v1", "tts": "eleven_multilingual_v2"},
}
