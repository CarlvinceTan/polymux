package vault

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// OTP accepts the reference vault's Base32 seeds and standard otpauth TOTP URIs.
func OTP(seed string, now time.Time) (CodeResult, error) {
	secret, algorithm, digits, period := seed, "SHA1", 6, int64(30)
	if strings.HasPrefix(strings.ToLower(seed), "otpauth://") {
		u, err := url.Parse(seed)
		if err != nil || u.Host != "totp" {
			return CodeResult{}, ErrInvalid
		}
		query := u.Query()
		secret = query.Get("secret")
		if query.Has("algorithm") {
			algorithm = strings.ToUpper(query.Get("algorithm"))
		}
		if query.Has("digits") {
			digits, err = strconv.Atoi(query.Get("digits"))
			if err != nil {
				return CodeResult{}, ErrInvalid
			}
		}
		if query.Has("period") {
			period, err = strconv.ParseInt(query.Get("period"), 10, 64)
			if err != nil {
				return CodeResult{}, ErrInvalid
			}
		}
	}
	if (digits != 6 && digits != 8) || period < 1 || period > 300 || now.Unix() < 0 {
		return CodeResult{}, ErrInvalid
	}
	secret = strings.TrimRight(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, secret), "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 10 || len(key) > 128 {
		return CodeResult{}, ErrInvalid
	}
	defer clear(key)
	var digest func() hash.Hash
	switch algorithm {
	case "SHA1":
		digest = sha1.New
	case "SHA256":
		digest = sha256.New
	case "SHA512":
		digest = sha512.New
	default:
		return CodeResult{}, ErrInvalid
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/period))
	mac := hmac.New(digest, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1_000_000)
	if digits == 8 {
		modulus = 100_000_000
	}
	return CodeResult{Code: fmt.Sprintf("%0*d", digits, value%modulus), ExpiresAt: (now.Unix()/period + 1) * period * 1000}, nil
}
