package cbor

import (
	"reflect"
	"sync"
)

// typeCache memoises the reflected field layout of a struct type. Encoding runs
// per protocol frame, so resolving tags once per type rather than per frame is
// worth the small amount of machinery.
type typeCache struct{ m sync.Map }

func newTypeCache() *typeCache { return &typeCache{} }

func (c *typeCache) load(t reflect.Type) ([]structField, bool) {
	v, ok := c.m.Load(t)
	if !ok {
		return nil, false
	}
	return v.([]structField), true
}

func (c *typeCache) store(t reflect.Type, fields []structField) {
	c.m.Store(t, fields)
}
