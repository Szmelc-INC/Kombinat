package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Szmelc-INC/Kombinat/core/config"
)

// TokenFile is the default token, relative to the secrets directory; its actor is "api".
const TokenFile = "api_token"

// tokenPrefix names per-actor tokens: secrets/token-ACTOR acts as ACTOR.
const tokenPrefix = "token-"

// minTokenLen rejects guessable tokens.
const minTokenLen = 32

// Tokens maps a bearer token to the actor it acts as.
type Tokens map[string]string

// LoadTokens reads secrets/api_token and every secrets/token-ACTOR file. When
// api_token is missing it is created with 32 random bytes (mode 0600) and
// created is true. Token values never leave this function except as map keys.
func LoadTokens(dir string) (tokens Tokens, created bool, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("secrets: %w", err)
	}
	def := filepath.Join(dir, TokenFile)
	if _, err := os.Stat(def); errors.Is(err, os.ErrNotExist) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, false, fmt.Errorf("secrets: generate token: %w", err)
		}
		if err := os.WriteFile(def, []byte(hex.EncodeToString(b)+"\n"), 0o600); err != nil {
			return nil, false, fmt.Errorf("secrets: write %s: %w", TokenFile, err)
		}
		created = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, fmt.Errorf("secrets: %w", err)
	}
	tokens = Tokens{}
	for _, e := range entries {
		name := e.Name()
		var actor string
		switch {
		case name == TokenFile:
			actor = "api"
		case strings.HasPrefix(name, tokenPrefix):
			actor = strings.TrimPrefix(name, tokenPrefix)
			if !config.ValidName(actor) {
				return nil, false, fmt.Errorf("secrets/%s: actor %q must be kebab-case", name, actor)
			}
		default:
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, false, fmt.Errorf("secrets/%s: %w", name, err)
		}
		tok := strings.TrimSpace(string(data))
		if len(tok) < minTokenLen || strings.ContainsAny(tok, " \t\r\n") {
			return nil, false, fmt.Errorf("secrets/%s: token must be one word of at least %d characters", name, minTokenLen)
		}
		if other, dup := tokens[tok]; dup {
			return nil, false, fmt.Errorf("secrets/%s: same token as actor %s", name, other)
		}
		tokens[tok] = actor
	}
	return tokens, created, nil
}

// Actors lists the actors, sorted, for logs that must not show tokens.
func (t Tokens) Actors() []string {
	out := make([]string, 0, len(t))
	for _, a := range t {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// match compares in constant time per candidate so a timing probe learns nothing.
func (t Tokens) match(got string) (string, bool) {
	actor, ok := "", false
	for tok, a := range t {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(got)) == 1 {
			actor, ok = a, true
		}
	}
	return actor, ok
}

type actorKey struct{}

// Actor returns the authenticated actor of a request.
func Actor(r *http.Request) string {
	a, _ := r.Context().Value(actorKey{}).(string)
	return a
}

// authenticate requires a known bearer token and stores its actor in the request.
func authenticate(tokens Tokens, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		got, found := strings.CutPrefix(h, "Bearer ")
		actor, ok := tokens.match(strings.TrimSpace(got))
		if !found || !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="kombinat"`)
			Error(w, http.StatusUnauthorized, "missing or invalid bearer token (see secrets/api_token)")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actor)))
	})
}
