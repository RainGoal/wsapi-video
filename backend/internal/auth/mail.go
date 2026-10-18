package auth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

func encodeSubject(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
func (s *Service) tokenURL(purpose, token string) string {
	path := "/verify-email"
	if purpose == "reset" {
		path = "/reset-password"
	}
	return strings.TrimRight(s.cfg.PublicURL, "/") + path + "?token=" + url.QueryEscape(token)
}

// SMTP uses a bounded connection and requires TLS away from localhost. The
// outbox retries delivery; secrets and ticket-bearing message bodies are never logged.
func sendMailContext(ctx context.Context, address string, authentication smtp.Auth, from, to string, message []byte) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline := time.Now().Add(20 * time.Second)
	if requestDeadline, ok := ctx.Deadline(); ok && requestDeadline.Before(deadline) {
		deadline = requestDeadline
	}
	_ = connection.SetDeadline(deadline)
	if port == "465" {
		tlsConnection := tls.Client(connection, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err = tlsConnection.HandshakeContext(ctx); err != nil {
			return err
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		} else if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return errors.New("SMTP server must support TLS")
		}
	}
	if authentication != nil {
		if err = client.Auth(authentication); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
