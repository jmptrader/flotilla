package flotilla

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	cNameSanitizer  = strings.NewReplacer("\n", "-", "\r", "-")
	cValueSanitizer = strings.NewReplacer("\n", " ", "\r", " ", ";", " ")
)

func cookies(c *ctx) map[string]*http.Cookie {
	ret := make(map[string]*http.Cookie)
	for _, cookie := range c.Request.Cookies() {
		ret[cookie.Name] = cookie
	}
	return ret
}

func readcookies(c *ctx) map[string]string {
	ret := make(map[string]string)
	cks := cookies(c)
	for k, v := range cks {
		ret[k] = unpackcookie(c, v)
	}
	return ret
}

func unpackcookie(c *ctx, cookie *http.Cookie) string {
	val := cookie.Value
	if val == "" {
		return val
	}

	parts := strings.SplitN(val, "|", 3)

	if len(parts) != 3 {
		return val
	}

	vs := parts[0]
	// timestamp := parts[1]
	sig := parts[2]

	if secret, ok := CheckStore(c, "SECRET_KEY"); ok {
		h := hmac.New(sha1.New, []byte(secret.Value))

		if fmt.Sprintf("%02x", h.Sum(nil)) != sig {
			return ""
		}

		res, _ := base64.URLEncoding.DecodeString(vs)
		return string(res)
	}
	return "cookie value could not be read and/or unpacked"
}

func cookie(c *ctx, secure bool, name string, value string, opts []interface{}) error {
	if secure {
		if secret, ok := CheckStore(c, "SECRET_KEY"); ok {
			value = securevalue(secret.Value, value)
		}
	}
	cke := basiccookie(name, value, opts...)
	headermodify(c, "add", []string{"Set-Cookie", cke})
	return nil
}

func securecookie(c *ctx, name string, value string, opts ...interface{}) error {
	return cookie(c, true, name, value, opts)
}

func securevalue(secret string, value string) string {
	vs := base64.URLEncoding.EncodeToString([]byte(value))
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	h := hmac.New(sha1.New, []byte(secret))
	sig := fmt.Sprintf("%02x", h.Sum(nil))
	cookie := strings.Join([]string{vs, timestamp, sig}, "|")
	return cookie
}

func basiccookie(name string, value string, opts ...interface{}) string {
	var b bytes.Buffer
	fmt.Fprintf(&b,
		"%s=%s",
		cNameSanitizer.Replace(name),
		cValueSanitizer.Replace(value))
	if len(opts) > 0 {
		if opt, ok := opts[0].(int); ok {
			if opt > 0 {
				fmt.Fprintf(&b, "; Max-Age=%d", opt)
			} else {
				fmt.Fprintf(&b, "; Max-Age=0")
			}
		}
	}
	if len(opts) > 1 {
		if opt, ok := opts[1].(string); ok && len(opt) > 0 {
			fmt.Fprintf(&b, "; Path=%s", cValueSanitizer.Replace(opt))
		}
	}
	if len(opts) > 2 {
		if opt, ok := opts[2].(string); ok && len(opt) > 0 {
			fmt.Fprintf(&b, "; Domain=%s", cValueSanitizer.Replace(opt))
		}
	}
	secure := false
	if len(opts) > 3 {
		if opt, ok := opts[3].(bool); ok {
			secure = opt
		}
	}
	if secure {
		fmt.Fprintf(&b, "; Secure")
	}
	httponly := false
	if len(opts) > 4 {
		if opt, ok := opts[4].(bool); ok {
			httponly = opt
		}
	}
	if httponly {
		fmt.Fprintf(&b, "; HttpOnly")
	}
	return b.String()
}

var cookiefxtension = map[string]interface{}{
	"cookie":       cookie,
	"securecookie": securecookie,
	"cookies":      cookies,
	"readcookies":  readcookies,
}

var CookieFxtension Fxtension = MakeFxtension("cookiefxtension", cookiefxtension)
