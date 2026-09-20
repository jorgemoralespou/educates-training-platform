package config

import (
	"sync"
	"time"
)

// apiServerVersionRefreshInterval is how long a version read is trusted for.
// The value changes when the control plane is upgraded and not otherwise, so
// this trades a slightly late notice of an upgrade against a round trip on
// every reconcile.
const apiServerVersionRefreshInterval = 10 * time.Minute

// apiServerVersionCache holds the API server version between reconciles.
//
// Reading it goes to the API server, which the capability probe would
// otherwise do on every reconcile, including the periodic resync, for a value
// which changes on upgrade. Caching it also keeps a brief failure to reach the
// API server from being reported as a probe failure, which would flip the
// mount condition and roll the session manager on a cluster which is fine.
type apiServerVersionCache struct {
	mutex sync.Mutex

	// refreshAfter is how long a read is trusted for. Zero selects
	// apiServerVersionRefreshInterval, so a zero valued cache embedded in
	// the reconciler still refreshes.
	refreshAfter time.Duration

	version string
	readAt  time.Time
}

// get returns the API server version, reading it through the given function
// when nothing has been read yet or the last read is older than the refresh
// interval.
//
// A failed refresh returns the last known value rather than an error: the
// version is a property of the cluster, not of this moment's connectivity. An
// error surfaces only when there is no known value to stand on.
func (c *apiServerVersionCache) get(read func() (string, error)) (string, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.version != "" && !c.due() {
		return c.version, nil
	}

	version, err := read()

	if err != nil {
		if c.version != "" {
			return c.version, nil
		}

		return "", err
	}

	c.version = version
	c.readAt = time.Now()

	return version, nil
}

// due reports whether the held value has outlived the refresh interval.
func (c *apiServerVersionCache) due() bool {
	interval := c.refreshAfter

	if interval <= 0 {
		interval = apiServerVersionRefreshInterval
	}

	return time.Since(c.readAt) >= interval
}

// expire marks the held value as due for a refresh on the next read, without
// discarding it, so a failed refresh can still fall back to it.
func (c *apiServerVersionCache) expire() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.readAt = time.Time{}
}
