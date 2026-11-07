package vault

import (
	"encoding/base32"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTOTPVectors(t *testing.T) {
	for _, tc := range []struct {
		name, secret string
		timestamp    int64
		code         string
	}{
		{"SHA1", "12345678901234567890", 59, "94287082"},
		{"SHA256", "12345678901234567890123456789012", 59, "46119246"},
		{"SHA512", "1234567890123456789012345678901234567890123456789012345678901234", 59, "90693936"},
		{"SHA1", "12345678901234567890", 20000000000, "65353130"},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.name, tc.timestamp), func(t *testing.T) {
			seed := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(tc.secret))
			uri := fmt.Sprintf("otpauth://totp/example?secret=%s&algorithm=%s&digits=8", seed, tc.name)
			code, err := OTP(uri, time.Unix(tc.timestamp, 0))
			if err != nil || code.Code != tc.code || code.ExpiresAt <= tc.timestamp*1000 {
				t.Fatal("RFC 6238 vector failed", err)
			}
		})
	}
	seed := strings.ToLower(base32.StdEncoding.EncodeToString([]byte("12345678901234567890")))
	code, err := OTP("  "+seed+" \n", time.Unix(59, 0))
	if err != nil || code.Code != "287082" {
		t.Fatal("reference Base32 behavior changed", err)
	}
	for _, seed := range []string{"bad", "otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP", "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&period=0", "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&digits=200"} {
		if _, err := OTP(seed, time.Now()); err == nil {
			t.Fatal("invalid TOTP accepted")
		}
	}
}
