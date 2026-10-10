package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	MinimumPasswordLength = 3
	argonMemory           = 64 * 1024
	argonIterations       = 3
	argonParallelism      = 2
	argonSaltLength       = 16
	argonKeyLength        = 32
)

func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < MinimumPasswordLength {
		return "", fmt.Errorf("密码长度不能少于 %d 位", MinimumPasswordLength)
	}

	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(encodedHash, password string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	version, err := parseParameter(parts[2], "v")
	if err != nil || version != argon2.Version {
		return false
	}
	memory, iterations, parallelism, err := parseArgonParameters(parts[3])
	if err != nil {
		return false
	}
	if memory != argonMemory || iterations != argonIterations || parallelism != argonParallelism {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}

	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parseArgonParameters(value string) (uint32, uint32, uint8, error) {
	items := strings.Split(value, ",")
	if len(items) != 3 {
		return 0, 0, 0, errors.New("argon2 参数格式错误")
	}
	memory, err := parseParameter(items[0], "m")
	if err != nil {
		return 0, 0, 0, err
	}
	iterations, err := parseParameter(items[1], "t")
	if err != nil {
		return 0, 0, 0, err
	}
	parallelism, err := parseParameter(items[2], "p")
	if err != nil || parallelism > 255 {
		return 0, 0, 0, errors.New("argon2 并行参数错误")
	}
	return uint32(memory), uint32(iterations), uint8(parallelism), nil
}

func parseParameter(value, name string) (int, error) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) {
		return 0, fmt.Errorf("缺少参数 %s", name)
	}
	return strconv.Atoi(strings.TrimPrefix(value, prefix))
}
