package store

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
}

func Open(ctx context.Context, databaseURL, redisURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	rOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("redis url: %w", err)
	}
	rc := redis.NewClient(rOpts)
	if err := rc.Ping(ctx).Err(); err != nil {
		pool.Close()
		_ = rc.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Store{DB: pool, Redis: rc}, nil
}

func (s *Store) Close() {
	if s.DB != nil {
		s.DB.Close()
	}
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
}

// ComputeHA1 returns MD5(username:realm:password) hex — used by Kamailio's
// auth_db with calculate_ha1=0.
func ComputeHA1(username, realm, password string) string {
	h := md5.Sum([]byte(username + ":" + realm + ":" + password))
	return hex.EncodeToString(h[:])
}

// ComputeHA1b returns MD5(username@realm:realm:password) hex — used when the
// digest username field includes the domain (some SIP clients do this).
func ComputeHA1b(username, realm, password string) string {
	h := md5.Sum([]byte(username + "@" + realm + ":" + realm + ":" + password))
	return hex.EncodeToString(h[:])
}
