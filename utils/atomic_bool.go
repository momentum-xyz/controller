package utils

import "sync/atomic"

type TAtomBool struct {
	flag int32
}

func (b *TAtomBool) Set(value bool) {
	var i int32 = 0
	if value {
		i = 1
	}

	atomic.StoreInt32(&(b.flag), int32(i))
}

func (b *TAtomBool) Get() bool {
	return atomic.LoadInt32(&(b.flag)) != 0
}

func (b *TAtomBool) Swap(value bool) bool {
	var i int32 = 0
	if value {
		i = 1
	}

	if atomic.SwapInt32(&(b.flag), i) != 0 {
		return true
	}
	return false
}
