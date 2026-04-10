package proto

import (
	"strconv"
	"sync/atomic"
	"time"
)

// ReqIDGen produces unique request IDs for one client connection.
// The format is "<startNanos>-<seq>" where startNanos is the
// generator's creation time (nanoseconds since unix epoch) and seq
// is a monotonic per-generator counter. Two requests from the same
// generator never collide; two requests from different generators
// only collide if both started at the exact same nanosecond AND
// happen to have the same seq.
//
// We deliberately avoid ULID/UUID dependencies — req_id correlation
// only needs uniqueness within a single client connection's
// in-flight set, which a per-client counter handles trivially.
type ReqIDGen struct {
	prefix string
	seq    atomic.Uint64
}

// NewReqIDGen returns a fresh generator stamped with the current
// time. Safe for concurrent use.
func NewReqIDGen() *ReqIDGen {
	return &ReqIDGen{
		prefix: strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

// Next returns a fresh request id.
func (g *ReqIDGen) Next() string {
	n := g.seq.Add(1)
	return g.prefix + "-" + strconv.FormatUint(n, 36)
}
