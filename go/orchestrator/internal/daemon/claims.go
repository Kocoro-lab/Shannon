package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrClaimNotFound = errors.New("claim not found")

const claimKeyPrefix = "shannon:daemon:claim:"

// ClaimMetadata stored alongside each claim in Redis.
type ClaimMetadata struct {
	ConnID        string `json:"conn_id"`
	ChannelID     string `json:"channel_id,omitempty"`
	ChannelType   string `json:"channel_type"`
	ThreadID      string `json:"thread_id,omitempty"`
	ReplyToken    string `json:"reply_token,omitempty"`
	Timestamp     string `json:"timestamp"`
	WorkflowID    string `json:"workflow_id,omitempty"`
	WorkflowRunID string `json:"workflow_run_id,omitempty"`
}

// ClaimManager coordinates message claims via Redis.
type ClaimManager struct {
	rc       redis.Cmdable
	claimTTL time.Duration // initial claim TTL (default 60s)
}

func NewClaimManager(rc redis.Cmdable) *ClaimManager {
	return &ClaimManager{rc: rc, claimTTL: 60 * time.Second}
}

// SetClaimTTL configures the initial claim TTL and heartbeat extension duration.
func (cm *ClaimManager) SetClaimTTL(ttl time.Duration) {
	if ttl > 0 {
		cm.claimTTL = ttl
	}
}

func claimKey(messageID string) string {
	return fmt.Sprintf("%s%s", claimKeyPrefix, messageID)
}

// TryClaim atomically claims a message. Returns true if this caller won.
func (cm *ClaimManager) TryClaim(ctx context.Context, messageID string, meta ClaimMetadata) (bool, error) {
	data, err := json.Marshal(meta)
	if err != nil {
		return false, err
	}
	ok, err := cm.rc.SetNX(ctx, claimKey(messageID), data, cm.claimTTL).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ExtendClaim extends the TTL on an existing claim (progress heartbeat).
func (cm *ClaimManager) ExtendClaim(ctx context.Context, messageID string, ttl time.Duration) error {
	return cm.rc.Expire(ctx, claimKey(messageID), ttl).Err()
}

// GetClaim retrieves claim metadata.
func (cm *ClaimManager) GetClaim(ctx context.Context, messageID string) (ClaimMetadata, error) {
	data, err := cm.rc.Get(ctx, claimKey(messageID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ClaimMetadata{}, ErrClaimNotFound
		}
		return ClaimMetadata{}, err
	}
	var meta ClaimMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return ClaimMetadata{}, err
	}
	return meta, nil
}

// ReleaseClaim deletes a claim (after reply or disconnect).
func (cm *ClaimManager) ReleaseClaim(ctx context.Context, messageID string) error {
	return cm.rc.Del(ctx, claimKey(messageID)).Err()
}

// ReleaseAllForConn releases all claims held by a specific connection.
func (cm *ClaimManager) ReleaseAllForConn(ctx context.Context, connID string) ([]string, error) {
	var released []string
	iter := cm.rc.Scan(ctx, 0, claimKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		data, err := cm.rc.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}
		var meta ClaimMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if meta.ConnID == connID {
			cm.rc.Del(ctx, key)
			msgID := key[len(claimKeyPrefix):]
			released = append(released, msgID)
		}
	}
	return released, iter.Err()
}
