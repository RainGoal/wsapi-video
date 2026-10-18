package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/config"
)

func locationHash(c config.Config) string {
	key := c.StorageDriver + "\x00" + c.S3Endpoint + "\x00" + c.S3Region + "\x00" + c.S3Bucket
	if c.StorageDriver == "local" {
		key = "local\x00" + c.StorageDir
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// LockStorage persists the location before the first object operation. It
// serializes with settings saves, including an upload's gap before asset INSERT.
// A failed first upload may conservatively lock the chosen location as well.
func (s *Service) LockStorage(ctx context.Context, c config.Config) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(794239106)"); err != nil {
		return err
	}
	st := s.defaults()
	var payload []byte
	err = tx.QueryRow(ctx, "SELECT payload FROM runtime_settings WHERE singleton").Scan(&payload)
	if err == nil {
		if err = s.open(payload, "runtime-settings", &st); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	current := locationHash(s.Config(st))
	if current != locationHash(c) {
		return errors.New("storage settings changed before object operation; retry")
	}
	_, err = tx.Exec(ctx, "INSERT INTO runtime_storage_lock(singleton,location_hash) VALUES(true,$1) ON CONFLICT DO NOTHING", current)
	if err != nil {
		return err
	}
	var locked string
	if err = tx.QueryRow(ctx, "SELECT location_hash FROM runtime_storage_lock WHERE singleton").Scan(&locked); err != nil {
		return err
	}
	if locked != current {
		return errors.New("storage location differs from persisted object location")
	}
	return tx.Commit(ctx)
}
