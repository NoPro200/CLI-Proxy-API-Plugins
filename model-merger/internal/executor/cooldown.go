package executor

import (
	"strings"
	"sync"
	"time"
)

// cooldowns remembers targets that recently failed, per virtual model, so the
// next requests start at the first target that is not cooling down. State is
// in memory only and resets when the plugin reloads.
type cooldowns struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   func() time.Time
}

func newCooldowns() *cooldowns {
	return &cooldowns{until: make(map[string]time.Time), now: time.Now}
}

func cooldownKey(alias, model string) string {
	return strings.ToLower(alias) + "\x00" + strings.ToLower(model)
}

func (c *cooldowns) start(alias, model string, seconds int) {
	if seconds <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until[cooldownKey(alias, model)] = c.now().Add(time.Duration(seconds) * time.Second)
}

func (c *cooldowns) clear(alias, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.until, cooldownKey(alias, model))
}

func (c *cooldowns) active(alias, model string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cooldownKey(alias, model)
	until, ok := c.until[key]
	if !ok {
		return false
	}
	if c.now().Before(until) {
		return true
	}
	delete(c.until, key)
	return false
}
