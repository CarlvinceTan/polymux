package vault

// Item is encrypted at rest, including its title, URL and field names.
type Item struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Title    string            `json:"title"`
	URL      string            `json:"url"`
	Fields   map[string]string `json:"fields"`
	Codes    []Code            `json:"codes"`
	Revision int64             `json:"revision"`
}

// Code tracks whether a recovery code has already been reserved for use.
type Code struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Used  bool   `json:"used"`
}

// Summary contains discoverable metadata, never field values or recovery codes.
type Summary struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Origin   string   `json:"origin"`
	Fields   []string `json:"fields"`
	Revision int64    `json:"revision"`
}

// Status describes initialization and unattended-key availability.
type Status struct {
	Created    bool   `json:"created"`
	Available  bool   `json:"available"`
	Protection string `json:"protection"`
}

// Browser selects an explicitly enabled loopback Chromium debugging endpoint.
type Browser struct {
	Endpoint string `json:"endpoint"`
}

// CodeResult contains a generated TOTP code and its expiry in Unix milliseconds.
type CodeResult struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expiresAt"`
}

// ImportResult reports counts without disclosing imported values.
type ImportResult struct {
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}
