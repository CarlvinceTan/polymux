package runtime

// Agent identifies a bot and its model selection; presentation belongs to the host.
type Agent struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Model     string   `json:"model"`
	Provider  string   `json:"provider,omitempty"`
	Reasoning string   `json:"reasoning,omitempty"`
	Fallback  Fallback `json:"fallback,omitzero"`
}

// How a bot moves off its model when that model can't answer.
const (
	FallbackAuto   = "auto"
	FallbackCustom = "custom"
	FallbackOff    = "off"
)

// Fallback is a bot's policy for a model that can't answer. The zero value is auto.
type Fallback struct {
	Mode   string          `json:"mode,omitempty"`
	Models []FallbackModel `json:"models,omitempty"`
}

// FallbackModel is one step of a custom order.
type FallbackModel struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning,omitempty"`
}

// Message contains the input consumed by one turn.
type Message struct {
	ID       string        `json:"id"`
	ThreadID string        `json:"threadID"`
	Role     string        `json:"role"`
	Parts    []Part        `json:"parts"`
	Reply    *ReplyContext `json:"reply,omitempty"`
}

// Part carries text or a host-managed attachment.
type Part struct {
	Kind       string      `json:"kind"`
	Text       *string     `json:"text,omitempty"`
	Attachment *Attachment `json:"attachment,omitempty"`
}

// Attachment names a file under the runtime's attachment directory.
type Attachment struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	MimeType      string  `json:"mimeType"`
	ExtractedText *string `json:"extractedText,omitempty"`
}

// AttachmentKindImage selects image content rather than a file reference.
const AttachmentKindImage = "image"

// ReplyContext preserves quoted-message identity and selected attachment context.
type ReplyContext struct {
	MessageID  string      `json:"messageID"`
	Role       string      `json:"role"`
	Text       string      `json:"text"`
	Attachment *Attachment `json:"attachment,omitempty"`
	AgentID    *string     `json:"agentID,omitempty"`
	PartIndex  *int64      `json:"partIndex,omitempty"`
}

// Usage reports the tokens billed for a turn.
type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}
