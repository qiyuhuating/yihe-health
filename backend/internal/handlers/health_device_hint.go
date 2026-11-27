package handlers

import (
	"hash/maphash"
	"sync"
	"time"

	"yihe-care/internal/devicecredential"
)

const healthDeviceHintTTL = 15 * time.Second
const healthDeviceHintSlots = 1024

// A bounded, direct-mapped cache of previously committed device bindings.
// It holds the stored credential hash, never the clear secret. A hit is only
// an admission/identity hint; the locked MySQL row authorizes every write.
type healthDeviceHints struct {
	mu    sync.Mutex
	seed  maphash.Seed
	slots [healthDeviceHintSlots]healthDeviceHint
}

type healthDeviceHint struct {
	device  healthDevice
	expires time.Time
}

func newHealthDeviceHints() *healthDeviceHints {
	return &healthDeviceHints{seed: maphash.MakeSeed()}
}

func (c *healthDeviceHints) get(no, secret string, epoch int64, now time.Time) *healthDevice {
	if c == nil {
		return nil
	}
	i := maphash.String(c.seed, no) % healthDeviceHintSlots
	c.mu.Lock()
	hint := c.slots[i]
	c.mu.Unlock()
	d := hint.device
	if d.No != no || d.OldID == nil || d.HealthEpoch != epoch || !now.Before(hint.expires) ||
		devicecredential.Check(secret, d.Secret, d.Status) != devicecredential.Valid {
		return nil
	}
	return &d
}

func (c *healthDeviceHints) put(d *healthDevice, now time.Time) {
	if c == nil {
		return
	}
	i := maphash.String(c.seed, d.No) % healthDeviceHintSlots
	c.mu.Lock()
	c.slots[i] = healthDeviceHint{device: *d, expires: now.Add(healthDeviceHintTTL)}
	c.mu.Unlock()
}

func (c *healthDeviceHints) remove(no string) {
	if c == nil {
		return
	}
	i := maphash.String(c.seed, no) % healthDeviceHintSlots
	c.mu.Lock()
	if c.slots[i].device.No == no {
		c.slots[i] = healthDeviceHint{}
	}
	c.mu.Unlock()
}
