// Package passwordhash implements argon2id password hashing for internal
// accounts (SPEC.md §8), using the standard
// $argon2id$v=19$m=...,t=...,p=...$salt$hash encoded format so parameters
// can change over time without invalidating already-hashed passwords.
package passwordhash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

type params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = params{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

// work bounds how many argon2id computations run at once, process-wide. Each
// one allocates `memory` KiB (64 MiB by default); without a cap a burst of
// concurrent logins/redemptions from a few clients could exhaust the pod's
// memory even though each client stays under the per-IP rate limit. Excess
// callers queue (their requests are bounded by the server timeout).
var work = make(chan struct{}, maxConcurrent())

func maxConcurrent() int {
	n := runtime.GOMAXPROCS(0)
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	return n
}

func derive(password string, salt []byte, p params) []byte {
	work <- struct{}{}
	defer func() { <-work }()
	return argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
}

var ErrMismatchedHashFormat = errors.New("passwordhash: unrecognized hash format")

func Hash(password string) (string, error) {
	p := defaultParams

	salt := make([]byte, p.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	key := derive(password, salt, p)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		p.memory, p.iterations, p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the given encoded hash, using a
// constant-time comparison to avoid leaking timing information about how
// much of the hash matched.
func Verify(password, encodedHash string) (bool, error) {
	p, salt, key, err := decode(encodedHash)
	if err != nil {
		return false, err
	}

	candidate := derive(password, salt, p)
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

func decode(encodedHash string) (params, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params{}, nil, nil, ErrMismatchedHashFormat
	}

	var p params
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != 19 {
		return params{}, nil, nil, ErrMismatchedHashFormat
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return params{}, nil, nil, ErrMismatchedHashFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("%w: decoding salt: %v", ErrMismatchedHashFormat, err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return params{}, nil, nil, fmt.Errorf("%w: decoding key: %v", ErrMismatchedHashFormat, err)
	}
	p.saltLength = uint32(len(salt))
	p.keyLength = uint32(len(key))

	return p, salt, key, nil
}
