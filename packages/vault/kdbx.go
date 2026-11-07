package vault

import (
	"bytes"
	"errors"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
)

const (
	KDBXLimit    = 32 << 20
	KeyFileLimit = 1 << 20
)

var (
	ErrKDBX       = errors.New("could not unlock the KDBX file")
	ErrKDBXFormat = errors.New("unsupported KDBX format")
)

func IsKDBX(data []byte) bool {
	return len(data) >= 8 &&
		data[0] == 0x03 && data[1] == 0xd9 && data[2] == 0xa2 && data[3] == 0x9a &&
		data[4] == 0x67 && data[5] == 0xfb && data[6] == 0x4b && data[7] == 0xb5
}

// ParseKDBX decrypts a KeePass database in memory and maps standard and custom
// fields. Attachments and history are not imported.
func ParseKDBX(data []byte, password string, keyFile []byte) (items []Item, err error) {
	defer func() {
		if recover() != nil {
			items, err = nil, ErrKDBX
		}
	}()
	if len(data) == 0 || len(data) > KDBXLimit || !IsKDBX(data) {
		return nil, ErrKDBX
	}
	if password == "" && len(keyFile) == 0 {
		return nil, ErrKDBX
	}
	if len(keyFile) > KeyFileLimit {
		return nil, ErrKDBX
	}
	db := gokeepasslib.NewDatabase()
	var credentials *gokeepasslib.DBCredentials
	switch {
	case len(keyFile) > 0 && password != "":
		credentials, err = gokeepasslib.NewPasswordAndKeyDataCredentials(password, keyFile)
	case len(keyFile) > 0:
		credentials, err = gokeepasslib.NewKeyDataCredentials(keyFile)
	default:
		credentials = gokeepasslib.NewPasswordCredentials(password)
	}
	if err != nil {
		return nil, ErrKDBX
	}
	db.Credentials = credentials
	if err := gokeepasslib.NewDecoder(bytes.NewReader(data)).Decode(db); err != nil {
		if errors.Is(err, gokeepasslib.ErrUnsupportedEncrypterType) || errors.Is(err, gokeepasslib.ErrUnsupportedStreamType) {
			return nil, ErrKDBXFormat
		}
		return nil, ErrKDBX
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		if errors.Is(err, gokeepasslib.ErrUnsupportedEncrypterType) || errors.Is(err, gokeepasslib.ErrUnsupportedStreamType) {
			return nil, ErrKDBXFormat
		}
		return nil, ErrKDBX
	}
	if db.Content == nil || db.Content.Root == nil {
		return nil, ErrKDBX
	}
	var recycle gokeepasslib.UUID
	if db.Content.Meta != nil {
		recycle = db.Content.Meta.RecycleBinUUID
	}
	items = []Item{}
	var walk func(gokeepasslib.Group, string, int) error
	walk = func(group gokeepasslib.Group, path string, depth int) error {
		if depth > 64 {
			return ErrImportContent
		}
		if !recycle.IsZero() && group.UUID.Compare(recycle) {
			return nil
		}
		name := strings.TrimSpace(group.Name)
		if name != "" {
			if path == "" {
				path = name
			} else {
				path += "/" + name
			}
		}
		for _, entry := range group.Entries {
			item, ok, err := kdbxItem(entry, path)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			items = append(items, item)
			if len(items) > 1000 {
				return ErrImportLimit
			}
		}
		for _, child := range group.Groups {
			if err := walk(child, path, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, group := range db.Content.Root.Groups {
		if err := walk(group, "", 0); err != nil {
			return nil, err
		}
	}
	if len(items) == 0 {
		return nil, ErrKDBX
	}
	return items, nil
}

type kdbxValue struct {
	key   string
	value string
	used  bool
}

func kdbxItem(entry gokeepasslib.Entry, group string) (Item, bool, error) {
	values := make([]kdbxValue, 0, len(entry.Values))
	for _, value := range entry.Values {
		values = append(values, kdbxValue{key: value.Key, value: value.Value.Content})
	}
	take := func(names ...string) (string, string) {
		for _, name := range names {
			for i := range values {
				if !values[i].used && strings.EqualFold(strings.TrimSpace(values[i].key), name) {
					values[i].used = true
					return values[i].value, values[i].key
				}
			}
		}
		return "", ""
	}

	title, _ := take("Title")
	username, _ := take("UserName", "Username", "Login Name", "Login")
	password, _ := take("Password")
	url, _ := take("URL", "Website", "Web Site")
	notesRaw, _ := take("Notes", "Note", "Comments")
	totpRaw, _ := take("otpauth", "OTP Auth", "TOTP Seed", "TOTP", "OTP", "otp")
	recoveryRaw, _ := take("recovery.codes", "Recovery Codes", "Recovery Code", "recovery codes", "Recovery")

	holder, _ := take("Cardholder", "Cardholder Name", "Holder", "Name on Card")
	number, _ := take("Card Number", "Number", "Credit Card Number")
	expiry, _ := take("Expiry", "Expiration", "Expiry Date", "Expiration Date")
	cvc, _ := take("CVC", "CVV", "Security Code", "Card Code", "Verification Number")
	pin, _ := take("PIN")
	holder, number, expiry = strings.TrimSpace(holder), strings.TrimSpace(number), strings.TrimSpace(expiry)
	cvc, pin = strings.TrimSpace(cvc), strings.TrimSpace(pin)
	isCard := number != "" || cvc != "" || pin != "" || (holder != "" && expiry != "")

	ssid, _ := take("SSID", "Network Name", "Network")
	security, _ := take("Security", "Encryption", "Wi-Fi Security", "WiFi Security")
	ssid, security = strings.TrimSpace(ssid), strings.TrimSpace(security)
	isWifi := ssid != ""

	token, _ := take("API Key", "API Token", "Token", "Access Token")
	token = strings.TrimSpace(token)

	credentialID, _ := take("KPEX_PASSKEY_CREDENTIAL_ID")
	privateKey, _ := take("KPEX_PASSKEY_PRIVATE_KEY_PEM")
	rpID, _ := take("KPEX_PASSKEY_RELYING_PARTY")
	passkeyUser, _ := take("KPEX_PASSKEY_USERNAME")
	userHandle, _ := take("KPEX_PASSKEY_USER_HANDLE")
	signCount, _ := take("VAULT_PASSKEY_SIGN_COUNT")
	credentialID, rpID = strings.TrimSpace(credentialID), strings.TrimSpace(rpID)
	passkeyUser, userHandle, signCount = strings.TrimSpace(passkeyUser), strings.TrimSpace(userHandle), strings.TrimSpace(signCount)
	passkeyPresent := credentialID != "" || rpID != "" || userHandle != "" || privateKey != "" || signCount != ""

	notes := new(noteText)
	passkeyNotes := func() {
		notes.addField("Passkey credential ID", credentialID)
		notes.addField("Passkey relying party", rpID)
		notes.addField("Passkey user handle", userHandle)
		notes.addField("Passkey private key", privateKey)
		notes.addField("Passkey sign count", signCount)
	}
	notes.add(notesRaw)
	if group != "" {
		notes.add("Group: " + group)
	}
	title = importTitle(title, notes)
	totp := importTOTP(totpRaw, notes)
	codes, err := importCodes(recoveryRaw)
	if err != nil {
		return Item{}, false, err
	}

	kind := "login"
	fields := map[string]string{
		"username": username, "password": password, "totp": totp, "token": token,
		"holder": holder, "number": number, "expiry": expiry, "cvc": cvc, "pin": pin,
		"ssid": ssid, "security": security,
	}
	activeURL := url
	switch {
	case credentialID != "" && privateKey != "" && rpID != "":
		passkeyFields := map[string]string{
			"username": passkeyUser, "rpID": rpID, "credentialID": credentialID,
			"userHandle": userHandle, "privateKey": privateKey, "signCount": signCount,
		}
		candidate := Item{
			ID: "import", Kind: "passkey", Title: title,
			URL: "https://" + rpID, Fields: passkeyFields, Codes: codes,
		}
		if Validate(candidate) == nil {
			kind = "passkey"
			if passkeyUser == "" {
				passkeyFields["username"] = username
			}
			fields = passkeyFields
			for key, value := range map[string]string{
				"password": password, "totp": totp, "token": token, "holder": holder,
				"number": number, "expiry": expiry, "cvc": cvc, "pin": pin,
				"ssid": ssid, "security": security,
			} {
				fields[key] = value
			}
			activeURL = candidate.URL
			if username != "" && username != passkeyFields["username"] {
				notes.addField("Username", username)
			}
			if url != "" && url != activeURL {
				notes.addField("URL", url)
			}
		} else {
			passkeyNotes()
		}
	case isCard:
		kind = "card"
	case isWifi:
		kind = "wifi"
	case token != "" && username == "" && password == "":
		kind = "api"
	case totp != "" && username == "" && password == "" && url == "" && len(codes) == 0:
		kind = "totp"
	case len(codes) > 0 && username == "" && password == "" && url == "" && totp == "":
		kind = "recovery"
	case username == "" && password == "" && url == "" && totp == "" && len(codes) == 0 && token == "" && !isCard && !isWifi:
		kind = "note"
	default:
		if passkeyPresent {
			passkeyNotes()
		}
	}
	for _, value := range values {
		if value.used || value.key == "" || value.value == "" {
			continue
		}
		notes.addField(value.key, value.value)
	}
	if tags := strings.TrimSpace(entry.Tags); tags != "" {
		notes.add("Tags: " + tags)
	}
	for _, binary := range entry.Binaries {
		if name := strings.TrimSpace(binary.Name); name != "" {
			notes.add("Attachment: " + name)
		}
	}
	item, err := importItem(kind, title, activeURL, fields, codes, notes)
	if err != nil {
		return Item{}, false, err
	}
	if strings.TrimSpace(entry.GetTitle()) == "" && len(entry.Values) == 0 && len(entry.Binaries) == 0 {
		return Item{}, false, nil
	}
	return item, true, nil
}
