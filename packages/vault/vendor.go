package vault

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bitwarden's unencrypted JSON export is the common vendor JSON format. Its
// encrypted JSON cannot be decrypted without Bitwarden's own account key.
func ParseVendorImport(data []byte) ([]Item, bool, error) {
	var probe struct {
		Encrypted *bool           `json:"encrypted"`
		Items     json.RawMessage `json:"items"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return nil, false, nil
	}
	if probe.Encrypted != nil && *probe.Encrypted {
		return nil, true, ErrVendorEncrypted
	}
	if len(probe.Items) == 0 || string(probe.Items) == "null" {
		return nil, false, nil
	}
	var export struct {
		Folders []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"folders"`
		Items []bitwardenEntry `json:"items"`
	}
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, true, fmt.Errorf("invalid Bitwarden JSON")
	}
	folders := map[string]string{}
	for _, folder := range export.Folders {
		folders[folder.ID] = strings.TrimSpace(folder.Name)
	}
	items := make([]Item, 0, len(export.Items))
	for _, entry := range export.Items {
		item, err := bitwardenItem(entry, folders)
		if err != nil {
			return nil, true, err
		}
		items = append(items, item)
		if len(items) > 1000 {
			return nil, true, ErrImportLimit
		}
	}
	return items, true, nil
}

type bitwardenEntry struct {
	Type        int              `json:"type"`
	Name        string           `json:"name"`
	Notes       string           `json:"notes"`
	FolderID    *string          `json:"folderId"`
	Login       *bitwardenLogin  `json:"login"`
	Card        *bitwardenCard   `json:"card"`
	Identity    map[string]any   `json:"identity"`
	Fields      []bitwardenField `json:"fields"`
	Attachments []struct {
		FileName string `json:"fileName"`
	} `json:"attachments"`
}

type bitwardenLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
	URIs     []struct {
		URI string `json:"uri"`
	} `json:"uris"`
}

type bitwardenCard struct {
	CardholderName string `json:"cardholderName"`
	Brand          string `json:"brand"`
	Number         string `json:"number"`
	ExpMonth       string `json:"expMonth"`
	ExpYear        string `json:"expYear"`
	Code           string `json:"code"`
}

type bitwardenField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func bitwardenItem(entry bitwardenEntry, folders map[string]string) (Item, error) {
	notes := new(noteText)
	if entry.FolderID != nil {
		if folder := folders[*entry.FolderID]; folder != "" {
			notes.add("Folder: " + folder)
		}
	}
	notes.add(entry.Notes)
	values := map[string]string{}
	codes := []Code{}
	kind := "note"
	url := ""

	switch entry.Type {
	case 1:
		kind = "login"
		if entry.Login != nil {
			values["username"] = entry.Login.Username
			values["password"] = entry.Login.Password
			values["totp"] = importTOTP(entry.Login.TOTP, notes)
			if len(entry.Login.URIs) > 0 {
				url = strings.TrimSpace(entry.Login.URIs[0].URI)
				for _, uri := range entry.Login.URIs[1:] {
					notes.add("URL: " + strings.TrimSpace(uri.URI))
				}
			}
		}
	case 3:
		kind = "card"
		if entry.Card != nil {
			values["holder"] = entry.Card.CardholderName
			values["number"] = entry.Card.Number
			values["cvc"] = entry.Card.Code
			expiry := entry.Card.ExpMonth
			if entry.Card.ExpYear != "" {
				expiry = strings.Trim(expiry+"/"+entry.Card.ExpYear, "/")
			}
			values["expiry"] = expiry
			notes.addField("Brand", entry.Card.Brand)
		}
	default:
		kind = "note"
	}
	for key, value := range entry.Identity {
		if text := fmt.Sprint(value); value != nil && strings.TrimSpace(text) != "" {
			notes.addField(key, text)
		}
	}
	for _, field := range entry.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || strings.TrimSpace(field.Value) == "" {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "recovery") && strings.Contains(lower, "code") {
			parsed, err := importCodes(field.Value)
			if err != nil {
				return Item{}, err
			}
			codes = append(codes, parsed...)
			continue
		}
		notes.addField(name, field.Value)
	}
	for _, attachment := range entry.Attachments {
		if name := strings.TrimSpace(attachment.FileName); name != "" {
			notes.add("Attachment: " + name)
		}
	}
	return importItem(kind, entry.Name, url, values, codes, notes)
}
