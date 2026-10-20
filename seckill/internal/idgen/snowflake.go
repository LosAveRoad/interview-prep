// Package idgen 实现雪花算法：1 位符号 + 41 位毫秒时间戳 + 10 位机器号 + 12 位序列号。
package idgen

import (
	"errors"
	"sync"
	"time"
)

const (
	nodeBits = 10
	seqBits  = 12
	maxNode  = -1 ^ (-1 << nodeBits)
	seqMask  = -1 ^ (-1 << seqBits)
	// epoch 自定义纪元（2024-01-01 UTC），41 位时间戳约可用 69 年。
	epoch = int64(1704067200000)
)

// ErrClockBackwards 表示检测到时钟回拨。
var ErrClockBackwards = errors.New("idgen: clock moved backwards")

// Snowflake 是并发安全的 ID 生成器。
type Snowflake struct {
	mu     sync.Mutex
	node   int64
	lastMs int64
	seq    int64
}

// New 创建生成器，node 取值 0~1023。
func New(node int64) (*Snowflake, error) {
	if node < 0 || node > maxNode {
		return nil, errors.New("idgen: node id out of range")
	}
	return &Snowflake{node: node}, nil
}

// Next 生成下一个 ID。时钟回拨 5ms 以内等待追平，超过则返回错误。
func (s *Snowflake) Next() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli() - epoch
	if now < s.lastMs {
		if s.lastMs-now > 5 {
			return 0, ErrClockBackwards
		}
		for now < s.lastMs {
			time.Sleep(time.Millisecond)
			now = time.Now().UnixMilli() - epoch
		}
	}
	if now == s.lastMs {
		s.seq = (s.seq + 1) & seqMask
		if s.seq == 0 { // 本毫秒序列号用完，等待下一毫秒
			for now <= s.lastMs {
				now = time.Now().UnixMilli() - epoch
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMs = now
	return now<<(nodeBits+seqBits) | s.node<<seqBits | s.seq, nil
}
