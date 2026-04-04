package storage

import (
	"fmt"
	"net"
	"time"
)

// RedisStore implements KVStore using Redis protocol.
// Uses raw TCP to avoid external dependency.
type RedisStore struct {
	addr     string
	password string
	db       int
	useTLS   bool
}

// NewRedisStore creates a new Redis KV store.
func NewRedisStore(addr, password string, db int, useTLS bool) (*RedisStore, error) {
	if addr == "" {
		addr = "localhost:6379"
	}

	r := &RedisStore{
		addr:     addr,
		password: password,
		db:       db,
		useTLS:   useTLS,
	}

	// Test connection
	if err := r.ping(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return r, nil
}

func (r *RedisStore) connect() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", r.addr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	// AUTH if password provided
	if r.password != "" {
		if _, err := r.sendCommand(conn, "AUTH", r.password); err != nil {
			conn.Close()
			return nil, err
		}
	}

	// SELECT database
	if r.db > 0 {
		if _, err := r.sendCommand(conn, "SELECT", fmt.Sprintf("%d", r.db)); err != nil {
			conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (r *RedisStore) sendCommand(conn net.Conn, args ...string) (string, error) {
	// Build RESP protocol command
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, arg := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg)
	}

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", err
	}

	// Read response
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}

	resp := string(buf[:n])
	if len(resp) > 0 && resp[0] == '-' {
		return "", fmt.Errorf("redis error: %s", resp[1:])
	}

	return resp, nil
}

func (r *RedisStore) ping() error {
	conn, err := r.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = r.sendCommand(conn, "PING")
	return err
}

func (r *RedisStore) Get(key string) ([]byte, bool, error) {
	conn, err := r.connect()
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()

	resp, err := r.sendCommand(conn, "GET", key)
	if err != nil {
		return nil, false, err
	}

	// Parse bulk string response
	if len(resp) > 0 && resp[0] == '$' {
		if resp[1] == '-' {
			return nil, false, nil // nil response = key not found
		}
		// Find the actual data after the length line
		dataStart := 0
		for i := 0; i < len(resp); i++ {
			if resp[i] == '\n' {
				dataStart = i + 1
				break
			}
		}
		if dataStart > 0 && dataStart < len(resp) {
			data := resp[dataStart:]
			// Trim trailing \r\n
			if len(data) >= 2 {
				data = data[:len(data)-2]
			}
			return []byte(data), true, nil
		}
	}

	return nil, false, nil
}

func (r *RedisStore) Set(key string, value []byte, ttl time.Duration) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	if ttl > 0 {
		_, err = r.sendCommand(conn, "SETEX", key, fmt.Sprintf("%d", int(ttl.Seconds())), string(value))
	} else {
		_, err = r.sendCommand(conn, "SET", key, string(value))
	}
	return err
}

func (r *RedisStore) Delete(key string) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = r.sendCommand(conn, "DEL", key)
	return err
}

func (r *RedisStore) Close() error { return nil }

func (r *RedisStore) Healthy() bool {
	return r.ping() == nil
}

// Verify interface compliance
var _ KVStore = (*RedisStore)(nil)
