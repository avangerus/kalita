package eventcore

import (
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

type ULIDGenerator struct {
	entropy *ulid.MonotonicEntropy
}

func NewULIDGenerator() *ULIDGenerator {
	src := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &ULIDGenerator{entropy: ulid.Monotonic(src, 0)}
}

func (g *ULIDGenerator) NewID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), g.entropy).String()
}
