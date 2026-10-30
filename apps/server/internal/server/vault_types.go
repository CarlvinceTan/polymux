package server

import (
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/packages/vault"
)

func vaultItem(item contract.VaultItem) vault.Item {
	var codes []vault.Code
	if item.Codes != nil {
		codes = make([]vault.Code, len(item.Codes))
		for n, code := range item.Codes {
			codes[n] = vault.Code(code)
		}
	}
	return vault.Item{
		ID: item.ID, Kind: item.Kind, Title: item.Title, URL: item.URL,
		Fields: item.Fields, Codes: codes, Revision: item.Revision,
	}
}

func wireItem(item vault.Item) contract.VaultItem {
	fields := make(map[string]string, len(item.Fields))
	for key, value := range item.Fields {
		if value != "" {
			fields[key] = ""
		}
	}
	codes := make([]contract.VaultCode, len(item.Codes))
	for n, code := range item.Codes {
		codes[n] = contract.VaultCode{ID: code.ID, Used: code.Used}
	}
	return contract.VaultItem{
		ID: item.ID, Kind: item.Kind, Title: item.Title, URL: item.URL,
		Fields: fields, Codes: codes, Revision: item.Revision, Removed: []string{},
	}
}
