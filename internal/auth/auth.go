package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/storage"
	"github.com/reviz-tw/Episteme/internal/storage/db"
	"golang.org/x/crypto/argon2"
	"strings"
	"time"
)

func Hash(password string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}
func Verify(hash, password string) bool {
	p := strings.Split(hash, "$")
	if len(p) != 5 || p[0] != "argon2id" || p[2] != "m=65536,t=3,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(p[3])
	if e != nil {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(p[4])
	if e != nil || len(expected) != 32 {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(expected, key) == 1
}
func TokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func Setup(ctx context.Context, s *storage.Store, username, password string) error {
	if len(username) < 3 || len(username) > 50 || len(password) < 12 || len(password) > 1024 {
		return errors.New("帳號需 3–50 字元，密碼至少 12 字元")
	}
	hash := Hash(password)
	return s.Transaction(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7160101)`); e != nil {
			return e
		}
		count, e := db.New(tx).CountUsers(ctx)
		if e != nil {
			return e
		}
		if count > 0 {
			return errors.New("管理員已建立")
		}
		_, e = tx.Exec(ctx, `INSERT INTO users(id,username,password_hash)VALUES($1,$2,$3)`, uuid.NewString(), username, hash)
		return e
	})
}
func Login(ctx context.Context, s *storage.Store, username, password string) (string, error) {
	var id, hash string
	e := s.Pool.QueryRow(ctx, `SELECT id::text,password_hash FROM users WHERE username=$1`, username).Scan(&id, &hash)
	if e != nil || !Verify(hash, password) {
		return "", errors.New("帳號或密碼錯誤")
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		return "", e
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	_, e = s.Pool.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at)VALUES($1,$2,$3)`, TokenHash(token), id, time.Now().Add(24*time.Hour))
	return token, e
}
func Username(ctx context.Context, s *storage.Store, token string) (string, error) {
	var name string
	e := s.Pool.QueryRow(ctx, `SELECT u.username FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=$1 AND expires_at>now()`, TokenHash(token)).Scan(&name)
	return name, e
}
