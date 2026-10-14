package antigravity

import (
	"encoding/binary"
	"errors"
	"math"
	"time"

	"opencode-dashboard/internal/stats"
)

// Minimal wire schema verified against Antigravity's embedded descriptors.
// Unknown fields are skipped; length/field limits prevent corrupt local blobs
// from allocating unbounded memory. No proprietary executable is loaded.
const maxBlobBytes = 32 << 20

type protoField struct {
	number int
	wire   byte
	value  uint64
	data   []byte
}
type protoMessage []protoField

func decodeProto(data []byte) (protoMessage, error) {
	if len(data) > maxBlobBytes {
		return nil, errors.New("protobuf blob exceeds limit")
	}
	result := make(protoMessage, 0, 16)
	for len(data) > 0 {
		if len(result) >= 8192 {
			return nil, errors.New("protobuf field limit exceeded")
		}
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return nil, errors.New("invalid protobuf tag")
		}
		data = data[n:]
		field := protoField{number: int(tag >> 3), wire: byte(tag & 7)}
		switch field.wire {
		case 0:
			value, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, errors.New("invalid protobuf integer")
			}
			field.value = value
			data = data[n:]
		case 1, 5:
			n := 8
			if field.wire == 5 {
				n = 4
			}
			if len(data) < n {
				return nil, errors.New("truncated protobuf number")
			}
			field.data = data[:n]
			data = data[n:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || size > uint64(len(data)-n) {
				return nil, errors.New("truncated protobuf field")
			}
			data = data[n:]
			field.data = data[:int(size)]
			data = data[int(size):]
		default:
			return nil, errors.New("unsupported protobuf wire type")
		}
		result = append(result, field)
	}
	return result, nil
}

func (m protoMessage) bytes(number int) []byte {
	for _, f := range m {
		if f.number == number && f.wire == 2 {
			return f.data
		}
	}
	return nil
}
func (m protoMessage) integer(number int) int64 {
	for _, f := range m {
		if f.number == number && f.wire == 0 && f.value <= math.MaxInt64 {
			return int64(f.value)
		}
	}
	return 0
}
func (m protoMessage) has(number int) bool {
	for _, f := range m {
		if f.number == number {
			return true
		}
	}
	return false
}
func (m protoMessage) text(number int) string                 { return string(m.bytes(number)) }
func (m protoMessage) child(number int) (protoMessage, error) { return decodeProto(m.bytes(number)) }

func protoTime(data []byte) time.Time {
	m, err := decodeProto(data)
	if err != nil || !m.has(1) {
		return time.Time{}
	}
	seconds, nanos := m.integer(1), m.integer(2)
	if seconds <= 0 || seconds > 253402300799 || nanos >= 1e9 {
		return time.Time{}
	}
	return time.Unix(seconds, nanos).UTC()
}

func usageTokens(data []byte) (*stats.TokenStats, error) {
	if len(data) == 0 {
		return nil, nil
	}
	m, err := decodeProto(data)
	if err != nil {
		return nil, err
	}
	for _, f := range m {
		switch f.number {
		case 2, 3, 4, 5, 9, 10:
			if f.wire != 0 {
				return nil, errors.New("unsupported token counter encoding")
			}
		}
		if f.wire == 0 && f.value > math.MaxInt64 {
			return nil, errors.New("token counter overflow")
		}
	}
	if !m.has(2) && !m.has(3) && !m.has(4) && !m.has(5) && !m.has(9) && !m.has(10) {
		return nil, errors.New("unsupported usage layout")
	}
	total, reasoning, response := m.integer(3), m.integer(9), m.integer(10)
	if m.has(10) && (response > total || reasoning > total || response != total-reasoning) {
		return nil, errors.New("inconsistent output counters")
	}
	if !m.has(10) {
		if reasoning > total {
			return nil, errors.New("inconsistent thinking counter")
		}
		response = total - reasoning
	}
	return &stats.TokenStats{Input: m.integer(2), Output: response, Reasoning: reasoning, Cache: stats.CacheStats{Read: m.integer(5), Write: m.integer(4)}}, nil
}
