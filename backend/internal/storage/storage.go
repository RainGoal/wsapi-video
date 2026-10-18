package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wsapi-video/internal/config"
)

type Store interface {
	Put(context.Context, string, io.Reader, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	SignedURL(context.Context, string, time.Duration) (string, error)
	Delete(context.Context, string) error
}

func New(c config.Config) (Store, error) {
	if c.StorageDriver == "s3" {
		return newS3(c), nil
	}
	if err := os.MkdirAll(c.StorageDir, 0700); err != nil {
		return nil, err
	}
	return &Local{root: c.StorageDir, origin: c.PublicURL, secret: []byte(c.AppSecret)}, nil
}

type Local struct {
	root, origin string
	secret       []byte
}

func (s *Local) path(key string) (string, error) {
	if key == "" || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") || strings.ContainsRune(key, 0) {
		return "", errors.New("invalid object key")
	}
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.Contains(p, ":") {
			return "", errors.New("invalid object key")
		}
	}
	path := filepath.Join(s.root, filepath.FromSlash(key))
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid object path")
	}
	return path, nil
}

func (s *Local) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, e := s.path(key)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return os.Open(p)
}
func (s *Local) Delete(ctx context.Context, key string) error {
	p, e := s.path(key)
	if e != nil {
		return e
	}
	e = os.Remove(p)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func (s *Local) signature(token, expires string) string {
	m := hmac.New(sha256.New, s.secret)
	_, _ = m.Write([]byte(token + "\n" + expires))
	return hex.EncodeToString(m.Sum(nil))
}
func (s *Local) SignedURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	if _, e := s.path(key); e != nil {
		return "", e
	}
	if expires <= 0 || expires > 24*time.Hour {
		return "", errors.New("invalid expiry")
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(key))
	end := strconv.FormatInt(time.Now().Add(expires).Unix(), 10)
	q := url.Values{"expires": {end}, "signature": {s.signature(token, end)}}
	return s.origin + "/api/v1/storage/" + token + "?" + q.Encode(), nil
}

func (s *Local) Register(mux *http.ServeMux) {
	serve := func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		end := r.URL.Query().Get("expires")
		sig, e := hex.DecodeString(r.URL.Query().Get("signature"))
		expected, _ := hex.DecodeString(s.signature(token, end))
		until, err := strconv.ParseInt(end, 10, 64)
		if e != nil || err != nil || until <= time.Now().Unix() || until > time.Now().Add(24*time.Hour).Unix() || !hmac.Equal(sig, expected) {
			http.NotFound(w, r)
			return
		}
		key, e := base64.RawURLEncoding.DecodeString(token)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		p, e := s.path(string(key))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, p)
	}
	mux.HandleFunc("GET /api/v1/storage/{token}", serve)
}
