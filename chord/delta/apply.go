package delta

import (
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
)

// ─── Applier ─────────────────────────────────────────────────────────────────

// Apply applies decoded ops to a plain mutable value and returns the result,
// because Replace replaces the value outright and cannot be done in place — and
// in Go a Splice or a Set one past the end re-headers a root slice for the same
// reason. Containers below the root are updated in place, so the caller's own
// reference to a map replica sees every write.
//
// The zero T is upstream's undefined target: a stream begins with a base batch,
// so nothing is read before a Replace has written, and a consumer needs no
// IsBase branch of its own. T is the replica's own type — any for a value of
// unknown shape, map[string]any or []any when it is known — and a stream whose
// Replace carries a different shape is an error rather than a panic.
//
// Replace, Set and Splice payloads are adopted, not copied: the consumer owns
// the batch it was handed. Fanning one batch out to several consumers
// in-process therefore makes their replicas alias each other. That is an
// ownership rule, not a defect: copy the batch at the fan-out point, let each
// consumer decode its own, or use ApplyImmutable. A batch that crosses a real
// boundary is already distinct, because serialisation produces fresh values.
//
// Apply is not transactional: ops before the failing one have already changed
// the replica. An error terminates the stream — discard the replica and its
// decoder and recover from a later base batch.
//
// Takes decoded ops. Path ids and omitted paths are a wire concern — run the
// decoder first if the ops came from a boundary.
func Apply[T any](target T, ops []Op) (T, error) {
	root, err := applyOps(target, ops, false)
	return typed[T](root, err)
}

// ApplyImmutable applies decoded ops without mutating the previous value. It
// copies only the containers along each op's path and shares every unchanged
// subtree, so one batch can safely fan out to several in-process consumers;
// it does not clone or freeze either complete input, and it treats op payloads
// as immutable rather than copying them.
func ApplyImmutable[T any](target T, ops []Op) (T, error) {
	root, err := applyOps(target, ops, true)
	return typed[T](root, err)
}

// typed returns root as the replica's own type. A nil root is the zero T: an
// untyped nil, a nil map or a nil slice.
func typed[T any](root any, err error) (T, error) {
	var zero T
	if err != nil || root == nil {
		return zero, err
	}
	v, ok := root.(T)
	if !ok {
		return zero, fmt.Errorf("delta: applied value is %T, not %T (the stream's Replace carried a different shape than the replica; apply with a matching type, or with any)", root, zero)
	}
	return v, nil
}

func applyOps(root any, ops []Op, clone bool) (any, error) {
	for _, op := range ops {
		if op == nil {
			return nil, fmt.Errorf("%w: op is nil (a batch is a []Op of Replace, Set, Delete, Append, Truncate or Splice; run ParseOp on wire input first)", ErrInvalidOp)
		}
		// Upstream's assertValidOp: the shape is the type's, the constraints
		// are Validate's — a root path where only "r" may have one, a negative
		// count, a reserved or negative segment.
		if err := op.Validate(); err != nil {
			return nil, err
		}
		next, err := applyOne(root, op, clone)
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func applyOne(root any, op Op, clone bool) (any, error) {
	switch op := op.(type) {
	case Replace:
		// Adopted, not copied. See Apply.
		return op.Value, nil
	case Splice:
		return walk(root, op.Path, clone, func(node any) (any, error) {
			xs, ok := node.([]any)
			if !ok {
				return nil, &PathError{Ref: op.Path}
			}
			return splice(xs, op.Index, op.Remove, op.Items), nil
		})
	case Set:
		return atParent(root, op.Path, clone, func(parent any, key Seg) (any, error) {
			return write(parent, key, op.Path, func(any) (any, error) { return op.Value, nil })
		})
	case Delete:
		return atParent(root, op.Path, clone, func(parent any, key Seg) (any, error) {
			return remove(parent, key, op.Path)
		})
	case Append:
		return atParent(root, op.Path, clone, func(parent any, key Seg) (any, error) {
			return write(parent, key, op.Path, func(current any) (any, error) {
				s, ok := current.(string)
				if !ok {
					return nil, &PathError{Ref: op.Path}
				}
				return s + op.Text, nil
			})
		})
	case Truncate:
		return atParent(root, op.Path, clone, func(parent any, key Seg) (any, error) {
			return write(parent, key, op.Path, func(current any) (any, error) {
				s, ok := current.(string)
				if !ok {
					return nil, &PathError{Ref: op.Path}
				}
				return truncateUTF16(s, op.Count), nil
			})
		})
	}
	// Op is sealed; a seventh verb is a bug in this package, not bad input.
	return nil, fmt.Errorf("%w: unknown op %T", ErrInvalidOp, op)
}

// ─── Walking ─────────────────────────────────────────────────────────────────

// walk descends path from root and hands fn the value it addresses; fn's result
// replaces that value. Every container on the way down is written back on the
// way up, so a slice that fn (or a child) re-headers stays attached to its
// parent. With clone set, each container along the path — the leaf included —
// is shallow-copied before it is written and the originals are left as they
// were: upstream's copyContainers, fused with the write it prepares for.
//
// The rules are upstream's resolveValue: own keys only, an index where the
// node is an array (a key there is unsafe, not merely unresolvable), and a
// PathError naming the whole path being resolved.
func walk(root any, path Path, clone bool, fn func(node any) (any, error)) (any, error) {
	var descend func(node any, rest Path) (any, error)
	descend = func(node any, rest Path) (any, error) {
		if clone {
			node = shallow(node)
		}
		if len(rest) == 0 {
			return fn(node)
		}
		seg := rest[0]
		switch c := node.(type) {
		case map[string]any:
			key := propertyKey(seg)
			child, ok := c[key]
			if !ok {
				return nil, &PathError{Ref: path}
			}
			next, err := descend(child, rest[1:])
			if err != nil {
				return nil, err
			}
			c[key] = next
			return c, nil
		case []any:
			i, ok := seg.(Index)
			if !ok {
				return nil, &UnsafePathError{Segment: seg}
			}
			if int(i) >= len(c) {
				return nil, &PathError{Ref: path}
			}
			next, err := descend(c[i], rest[1:])
			if err != nil {
				return nil, err
			}
			c[i] = next
			return c, nil
		}
		return nil, &PathError{Ref: path}
	}
	return descend(root, path)
}

// atParent resolves an s/d/a/t op's parent — the path less its last segment,
// which Validate has already guaranteed exists — and hands fn the container
// and that last segment. The parent must be a container: upstream's resolve
// reports anything else as an unresolvable parent path.
func atParent(root any, path Path, clone bool, fn func(parent any, key Seg) (any, error)) (any, error) {
	parent := path[:len(path)-1]
	return walk(root, parent, clone, func(node any) (any, error) {
		switch c := node.(type) {
		case map[string]any:
			if c == nil {
				// The zero replica: apply(undefined, [s]) resolves nothing.
				return nil, &PathError{Ref: parent}
			}
		case []any:
		default:
			return nil, &PathError{Ref: parent}
		}
		return fn(node, path[len(path)-1])
	})
}

// shallow copies one container level; anything else is returned as is.
func shallow(node any) any {
	switch c := node.(type) {
	case map[string]any:
		return maps.Clone(c)
	case []any:
		return slices.Clone(c)
	}
	return node
}

// ─── Leaf writes ─────────────────────────────────────────────────────────────

// write reads parent[key] — nil when absent, upstream's own-property read of
// undefined — hands fn the current value and stores what fn returns. Under an
// object the key is the segment spelled as a string, as JavaScript coerces it. Under an array the segment
// must be an index, and — upstream's assertIndexInRange — it may address an
// existing element or append exactly one past the end. Not an arbitrary cap:
// a sparse array does not survive a JSON round trip, so a gap already produces
// state a replica cannot match, and one op could otherwise allocate a
// 4.29-billion-entry array. Growth stays possible and stays proportional: the
// tracker emits it as a splice of explicit nulls whose size grows with the gap.
func write(parent any, key Seg, path Path, fn func(current any) (any, error)) (any, error) {
	switch p := parent.(type) {
	case map[string]any:
		k := propertyKey(key)
		v, err := fn(p[k])
		if err != nil {
			return nil, err
		}
		p[k] = v
		return p, nil
	case []any:
		i, err := arrayIndex(p, key)
		if err != nil {
			return nil, err
		}
		if i == len(p) {
			v, err := fn(nil)
			if err != nil {
				return nil, err
			}
			return append(p, v), nil
		}
		v, err := fn(p[i])
		if err != nil {
			return nil, err
		}
		p[i] = v
		return p, nil
	}
	return nil, &PathError{Ref: path[:len(path)-1]}
}

// remove is "d": delete an object property, or an array element that exists.
// Deleting a missing property is a no-op, as `delete` is; deleting one past
// an array's end is unresolvable, and further out is the index rule.
func remove(parent any, key Seg, path Path) (any, error) {
	switch p := parent.(type) {
	case map[string]any:
		delete(p, propertyKey(key))
		return p, nil
	case []any:
		i, err := arrayIndex(p, key)
		if err != nil {
			return nil, err
		}
		if i >= len(p) {
			return nil, &PathError{Ref: path}
		}
		return slices.Delete(p, i, i+1), nil
	}
	return nil, &PathError{Ref: path[:len(path)-1]}
}

// propertyKey is the segment as an object property name: a Key as is, an
// Index spelled out, the way JavaScript coerces parent[3] to parent["3"].
func propertyKey(seg Seg) string {
	switch s := seg.(type) {
	case Key:
		return string(s)
	case Index:
		return s.String()
	}
	return fmt.Sprint(seg)
}

// arrayIndex checks a leaf segment against an array: it must be an index no
// further than one past the end.
func arrayIndex(xs []any, key Seg) (int, error) {
	i, ok := key.(Index)
	if !ok {
		return 0, &UnsafePathError{Segment: key}
	}
	if int(i) > len(xs) {
		return 0, &UnsafePathError{Segment: key}
	}
	return int(i), nil
}

// splice is Array.prototype.splice(index, remove, ...items) on xs: an index
// past the end is clamped to it, a remove count past the end is clamped to what
// is there — deterministic and identical on both sides, never a hole — and the
// items are inserted as they are. There is no chunking: that was a JavaScript
// spread-argument limit, and slices.Insert takes any number.
func splice(xs []any, index, remove int, items []any) []any {
	index = min(index, len(xs))
	remove = min(remove, len(xs)-index)
	xs = slices.Delete(xs, index, index+remove)
	return slices.Insert(xs, index, items...)
}

// ─── Strings ─────────────────────────────────────────────────────────────────

// truncateUTF16 is s.slice(count) for a JavaScript string: it drops count
// UTF-16 code units from the front — the wire's unit, which a Go replica must
// honour or the two sides of a rolling window drift on the first non-ASCII
// character. A count past the end leaves the empty string.
//
// A count that splits a surrogate pair leaves U+FFFD in place of the orphaned
// low half. pi holds a lone surrogate there, which no Go string can; U+FFFD is
// what encoding/json makes of that lone surrogate once it crosses the wire, so
// the Go replica holds exactly what a Go decoder would read from the pi one,
// and later counts agree — one unit either way.
func truncateUTF16(s string, count int) string {
	i := 0
	for count > 0 && i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if utf16.RuneLen(r) == 2 && count == 1 {
			return "�" + s[i+size:]
		}
		count -= utf16.RuneLen(r)
		i += size
	}
	return s[i:]
}
