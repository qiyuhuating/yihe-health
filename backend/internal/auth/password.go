// Package auth 内的密码工具：复刻 PasswordUtil：BCrypt cost=12；新密码 12–20 位含字母+数字；
// matches 只认 $2a$/$2b$/$2y$ 开头的已哈希值。
package auth

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const Cost = 12

var (
	phoneRe = regexp.MustCompile(`^1\d{10}$`)
	// 注意：新密码规则不用正则实现（RE2 不支持前瞻），见 ValidNewPassword。
	hashPrefix = []string{"$2a$", "$2b$", "$2y$"}
)

// Hash 生成 BCrypt 哈希。
func Hash(plain string) (string, error) {
	if len(plain) > 72 {
		return "", bcrypt.ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), Cost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Matches 校验明文密码；非哈希存量一律不匹配。
func Matches(plain, hashed string) bool {
	if plain == "" || len(plain) > 72 {
		return false
	}
	ok := false
	for _, p := range hashPrefix {
		if strings.HasPrefix(hashed, p) {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// ValidNewPassword 新密码规则：12–20 位、同时含字母和数字、UTF-8 ≤72 字节。
// 逐字节检查密码是否同时包含 ASCII 字母和数字。
func ValidNewPassword(pw string) bool {
	n := utf8.RuneCountInString(pw)
	if n < 12 || n > 20 {
		return false
	}
	if !utf8.ValidString(pw) || len(pw) > 72 { // UTF-8 字节数 ≤72（BCrypt 上限）
		return false
	}
	hasLetter, hasDigit := false, false
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			hasLetter = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return true
		}
	}
	return false
}

// ValidPhone 手机号规则：^1\d{10}$。
func ValidPhone(phone string) bool {
	return phoneRe.MatchString(phone)
}
