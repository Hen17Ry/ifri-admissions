// Package ratelimit counts attempts and failures in Redis with atomic Lua scripts: an attempt
// cap per window, then a progressive delay after free failures, then a temporary lock.
// Keys are pseudonyms (HMAC) built by the caller: no email or IP address reaches Redis.
package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Policy struct {
	Name         string
	MaxAttempts  int           // attempts (any outcome) per window
	Window       time.Duration // counting window
	FreeFailures int           // failures without delay
	BaseDelay    time.Duration // delay after the first counted failure, doubled each time
	MaxDelay     time.Duration
	LockAfter    int           // failures that lock the key
	LockFor      time.Duration // lock duration
}

// Limiter is checked before any expensive work (argon2, TOTP decryption).
type Limiter interface {
	// Attempt counts one attempt; a positive duration means blocked, retry after it.
	Attempt(ctx context.Context, p Policy, key string) (time.Duration, error)
	// Failure records a failed verification and returns the delay now imposed.
	Failure(ctx context.Context, p Policy, key string) (time.Duration, error)
	// Reset clears a key after a success.
	Reset(ctx context.Context, p Policy, key string) error
}

var attemptScript = redis.NewScript(`
local now, max, window, lock = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local blocked = tonumber(redis.call('HGET', KEYS[1], 'blocked_until') or '0')
if blocked > now then return blocked - now end
local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if attempts == 1 then redis.call('PEXPIRE', KEYS[1], window) end
if attempts > max then
  redis.call('HSET', KEYS[1], 'blocked_until', now + lock)
  if redis.call('PTTL', KEYS[1]) < lock then redis.call('PEXPIRE', KEYS[1], lock) end
  return lock
end
return 0`)

var failureScript = redis.NewScript(`
local now, free, base, maxdelay, lockafter, lock, window = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7])
local failures = redis.call('HINCRBY', KEYS[1], 'failures', 1)
local delay = 0
if failures >= lockafter then delay = lock
elseif failures > free then delay = math.min(base * 2 ^ (failures - free - 1), maxdelay) end
if delay > 0 then redis.call('HSET', KEYS[1], 'blocked_until', now + delay) end
local need = math.max(window, delay)
if redis.call('PTTL', KEYS[1]) < need then redis.call('PEXPIRE', KEYS[1], need) end
return delay`)

type Redis struct {
	Client redis.Scripter
	Prefix string
	Now    func() time.Time
}

func (r Redis) key(p Policy, key string) string { return r.Prefix + p.Name + ":" + key }
func (r Redis) now() int64 {
	if r.Now != nil {
		return r.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

func (r Redis) Attempt(ctx context.Context, p Policy, key string) (time.Duration, error) {
	ms, err := attemptScript.Run(ctx, r.Client, []string{r.key(p, key)}, r.now(), p.MaxAttempts, p.Window.Milliseconds(), p.LockFor.Milliseconds()).Int64()
	return time.Duration(ms) * time.Millisecond, err
}

func (r Redis) Failure(ctx context.Context, p Policy, key string) (time.Duration, error) {
	ms, err := failureScript.Run(ctx, r.Client, []string{r.key(p, key)}, r.now(), p.FreeFailures, p.BaseDelay.Milliseconds(), p.MaxDelay.Milliseconds(), p.LockAfter, p.LockFor.Milliseconds(), p.Window.Milliseconds()).Int64()
	return time.Duration(ms) * time.Millisecond, err
}

func (r Redis) Reset(ctx context.Context, p Policy, key string) error {
	return redis.NewScript(`return redis.call('DEL', KEYS[1])`).Run(ctx, r.Client, []string{r.key(p, key)}).Err()
}
