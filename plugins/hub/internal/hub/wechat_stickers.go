package hub

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	stickerLimit  = 8 << 20
	pictureLimit  = 25 << 20
	stickerWait   = 30 * time.Second
	pictureSend   = 130 * time.Second
	pictureEcho   = "\x00picture"
	stickerNotice = "Sticker"
)

var (
	emojiElement  = regexp.MustCompile(`(?is)<emoji\b[^>]*/>|<emoji\b.*?</emoji>`)
	emojiDigest   = regexp.MustCompile(`(?i)\bmd5\s*=\s*["']([a-f0-9]{32})["']`)
	emojiAddress  = regexp.MustCompile(`(?i)cdnurl\s*=\s*"([^"]+)"`)
	emojiWidth    = regexp.MustCompile(`(?i)\bwidth\s*=\s*"(\d+)"`)
	emojiHeight   = regexp.MustCompile(`(?i)\bheight\s*=\s*"(\d+)"`)
	appType       = regexp.MustCompile(`<type>(?:<!\[CDATA\[)?(\d+)`)
	appDigest     = regexp.MustCompile(`<emoticonmd5>(?:<!\[CDATA\[)?([A-Fa-f0-9]{32})`)
	appInfo       = regexp.MustCompile(`<emojiinfo>(?:<!\[CDATA\[)?([A-Za-z0-9+/=]+)`)
	imageSuffixes = map[string]string{"image/gif": ".gif", "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}
)

// stickerRef is where a sticker's picture is. Its digest proves the download is that sticker.
type stickerRef struct {
	digest, address string
	width, height   int64
}

// savedSticker is a sticker already on the homeserver, since Desktop's own sends carry only a digest.
type savedSticker struct {
	URI    string `json:"uri"`
	Mime   string `json:"mime"`
	Size   int64  `json:"size"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

func attribute(pattern *regexp.Regexp, text string) int64 {
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return 0
	}
	value, _ := strconv.ParseInt(match[1], 10, 64)
	return value
}

// appSticker reads appmsg type 8, whose reference is a small protobuf: field 1 is the digest, field 2 the address.
func appSticker(body string) (stickerRef, bool) {
	kind := appType.FindStringSubmatch(body)
	digest := appDigest.FindStringSubmatch(body)
	if len(body) > 128<<10 || kind == nil || kind[1] != "8" || digest == nil {
		return stickerRef{}, false
	}
	value := stickerRef{digest: strings.ToLower(digest[1])}
	info := appInfo.FindStringSubmatch(body)
	if info == nil {
		return value, true
	}
	data, err := base64.StdEncoding.DecodeString(info[1])
	if err != nil {
		return stickerRef{}, false
	}
	fields := map[uint64]string{}
	for at := 0; at < len(data); {
		tag, size := uvarint(data[at:])
		if size == 0 {
			return stickerRef{}, false
		}
		at += size
		switch tag & 7 {
		case 0:
			_, size = uvarint(data[at:])
			if size == 0 {
				return stickerRef{}, false
			}
			at += size
		case 1:
			at += 8
		case 5:
			at += 4
		case 2:
			length, size := uvarint(data[at:])
			at += size
			if size == 0 || length > uint64(len(data)-at) {
				return stickerRef{}, false
			}
			fields[tag>>3] = string(data[at : at+int(length)])
			at += int(length)
		default:
			return stickerRef{}, false
		}
	}
	if fields[1] != value.digest {
		return stickerRef{}, false
	}
	value.address = fields[2]
	return value, true
}

func uvarint(data []byte) (uint64, int) {
	var value uint64
	for index, item := range data {
		if index > 9 {
			return 0, 0
		}
		value |= uint64(item&0x7f) << (7 * index)
		if item&0x80 == 0 {
			return value, index + 1
		}
	}
	return 0, 0
}

func parseSticker(body string) (stickerRef, bool) {
	element := emojiElement.FindString(body)
	if element == "" {
		return appSticker(body)
	}
	value := stickerRef{width: attribute(emojiWidth, element), height: attribute(emojiHeight, element)}
	if match := emojiDigest.FindStringSubmatch(element); match != nil {
		value.digest = strings.ToLower(match[1])
	}
	if match := emojiAddress.FindStringSubmatch(element); match != nil {
		value.address = html.UnescapeString(match[1])
	}
	return value, true
}

// imageKind goes by the bytes: WeChat's CDN calls everything octet-stream, and most stickers are GIFs.
func imageKind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "image/gif"
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case len(data) > 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// publicClient refuses private addresses, because a sticker's address comes from whoever sent it.
func publicClient() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return errors.New("that address isn't public")
		}
		return nil
	}}
	return &http.Client{Timeout: stickerWait, Transport: &http.Transport{DialContext: dialer.DialContext}}
}

func (w *wechat) fetchSticker(ctx context.Context, ref stickerRef) ([]byte, string, error) {
	address, err := url.Parse(ref.address)
	if err != nil || (address.Scheme != "http" && address.Scheme != "https") || address.User != nil {
		return nil, "", errors.New("sticker has no usable address")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := w.media.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", remoteError{status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, stickerLimit+1))
	if err != nil {
		return nil, "", err
	}
	kind := imageKind(data)
	sum := md5.Sum(data)
	if len(data) > stickerLimit || kind == "" || (ref.digest != "" && hex.EncodeToString(sum[:]) != ref.digest) {
		return nil, "", errors.New("that isn't the sticker it claims to be")
	}
	return data, kind, nil
}

func (w *wechat) upload(ctx context.Context, data []byte, kind string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/_matrix/media/v3/upload?filename=sticker", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+w.asToken)
	request.Header.Set("Content-Type", kind)
	response, err := w.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		URI string `json:"content_uri"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	if !strings.HasPrefix(result.URI, "mxc://") {
		return "", errors.New("upload gave no address")
	}
	return result.URI, nil
}

// stickerContent turns a WeChat sticker into the room's own. Nil leaves it as the word Sticker.
func (w *wechat) stickerContent(ctx context.Context, body string) map[string]any {
	ref, ok := parseSticker(body)
	if !ok {
		return nil
	}
	w.mu.Lock()
	saved, known := w.saved.Stickers[ref.digest]
	w.mu.Unlock()
	if !known {
		ctx, cancel := context.WithTimeout(ctx, stickerWait)
		defer cancel()
		data, kind, err := w.fetchSticker(ctx, ref)
		if err == nil {
			saved.URI, err = w.upload(ctx, data, kind)
		}
		if err != nil {
			w.log.Warn("wechat sticker", "error", err)
			return nil
		}
		saved.Mime, saved.Size, saved.Width, saved.Height = kind, int64(len(data)), ref.width, ref.height
		if ref.digest != "" {
			w.mu.Lock()
			w.saved.Stickers[ref.digest] = saved
			w.mu.Unlock()
		}
	}
	return map[string]any{"msgtype": "m.image", "body": stickerNotice, "url": saved.URI, "co.polymux.sticker": true, "co.polymux.wechat.remote": true,
		"info": map[string]any{"mimetype": saved.Mime, "size": saved.Size, "w": saved.Width, "h": saved.Height}}
}

func (w *wechat) download(ctx context.Context, uri string) ([]byte, string, error) {
	rest, found := strings.CutPrefix(uri, "mxc://")
	server, id, split := strings.Cut(rest, "/")
	if !found || !split || strings.Contains(id, "/") {
		return nil, "", errors.New("no usable media address")
	}
	path := fmt.Sprintf("%s/_matrix/client/v1/media/download/%s/%s", w.base, url.PathEscape(server), url.PathEscape(id))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Authorization", "Bearer "+w.asToken)
	response, err := w.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", remoteError{status: response.StatusCode}
	}
	return readPicture(response.Body)
}

func readPicture(body io.Reader) ([]byte, string, error) {
	data, err := io.ReadAll(io.LimitReader(body, pictureLimit+1))
	if err != nil {
		return nil, "", err
	}
	kind := imageKind(data)
	if len(data) > pictureLimit || kind == "" {
		return nil, "", errors.New("that isn't a picture WeChat can take")
	}
	return data, kind, nil
}

// onDisk saves a picture for as long as the relay's CLI needs it.
func onDisk(data []byte, kind string, use func(path string) error) error {
	folder, err := os.MkdirTemp("", "hub-wechat-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(folder)
	path := filepath.Join(folder, "picture"+imageSuffixes[kind])
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return use(path)
}

// sendImage hands WeChat one picture through the relay's CLI. WeChat gives it no delivery receipt,
// so being submitted counts. Nothing here resends, since a repeat could post the picture twice.
func sendImage(ctx context.Context, cli, chat, path string) error {
	ctx, cancel := context.WithTimeout(ctx, pictureSend)
	defer cancel()
	// It exits non-zero without a receipt, so its JSON is the answer.
	output, err := exec.CommandContext(ctx, cli, "send", "--image", path, "--wxid", chat, "--json").Output()
	var result struct {
		Sent   bool   `json:"sent"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(output, &result) != nil {
		return errors.Join(errors.New("wechat-use gave no answer"), err)
	}
	if result.Reason == "wechat_in_foreground" {
		return publicError("Switch away from WeChat, then try again.")
	}
	if !result.Sent {
		return fmt.Errorf("picture not sent: %s", result.Reason)
	}
	return nil
}

func (w *wechat) pastePicture(ctx context.Context, chat, path string) error {
	return sendImage(ctx, filepath.Join(filepath.Dir(w.binary), "wechat-use"), chat, path)
}

// sendPicture carries a picture or sticker. WeChat receives a sticker as a picture of it.
func (w *wechat) sendPicture(ctx context.Context, room, uri string) error {
	chat := w.chatOf(room)
	if chat == "" {
		return nil
	}
	data, kind, err := w.download(ctx, uri)
	if err != nil {
		return err
	}
	if err := onDisk(data, kind, func(path string) error { return w.paste(ctx, chat, path) }); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved.Echoes = append(w.saved.Echoes, wechatEcho{Chat: chat, Body: pictureEcho, At: time.Now().UnixMilli()})
	return w.save()
}

// adoptedPicture carries a picture past an external bridge, which only takes text. WeChat gets it
// first, so the thread never shows what didn't go.
func (s *service) adoptedPicture(ctx context.Context, client *matrix, chat string, file *contract.HubAttachment) error {
	s.mu.Lock()
	fleet := s.bridges
	s.mu.Unlock()
	id := adoptedChat(fleet.dir, chat, client.portalHash(chat))
	if id == "" || s.picture == nil {
		return publicError("This chat can't take pictures.")
	}
	response, err := client.fetch(ctx, file.Uri, false)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, kind, err := readPicture(response.Body)
	if err != nil {
		return publicError("WeChat only takes pictures here.")
	}
	return onDisk(data, kind, func(path string) error { return s.picture(ctx, id, path) })
}

func (s *service) post(ctx context.Context, client *matrix, chat string, input contract.HubSend) error {
	s.mu.Lock()
	adopted := s.bridges != nil && s.bridges.adopted
	s.mu.Unlock()
	if adopted && input.Attachment != nil && client.chatPlatform(chat) == wechatPlatform {
		if err := s.adoptedPicture(ctx, client, chat, input.Attachment); err != nil {
			return err
		}
	}
	_, err := client.send(ctx, chat, input)
	return err
}
