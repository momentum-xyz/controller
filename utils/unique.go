package utils

type Unique[V any] struct {
	val V
	cmp *struct{}
}

func NewUnique[V any](val V) Unique[V] {
	return Unique[V]{
		val: val,
		cmp: new(struct{}),
	}
}

func (u Unique[V]) Value() V {
	return u.val
}

func (u Unique[V]) Equal(unique Unique[V]) bool {
	return u.cmp == unique.cmp
}
