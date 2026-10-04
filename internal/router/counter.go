package router

import "sync/atomic"

type counter struct{ n atomic.Uint64 }

func (c *counter) add()         { c.n.Add(1) }
func (c *counter) load() uint64 { return c.n.Load() }
