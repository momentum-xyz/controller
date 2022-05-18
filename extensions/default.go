package extensions

import (
	"sort"

	"github.com/google/uuid"
	"github.com/momentum-xyz/controller/internal/extension"
)

func Default() extension.Extension {
	return &defaultExtension{}
}

// Make sure defaultExtension always implements Extension (compile time error)
var _ extension.Extension = &defaultExtension{}

type defaultExtension struct {
	world extension.WorldController
}

func (e *defaultExtension) Init() bool {
	return true
}

func (e *defaultExtension) Run() {
}

func (e *defaultExtension) SortSpaces(s []uuid.UUID, t uuid.UUID) {
	sort.Slice(s, func(i, j int) bool { return s[i].ClockSequence() < s[j].ClockSequence() })
}

func (e *defaultExtension) InitSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}

func (e *defaultExtension) DeinitSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}

func (e *defaultExtension) InitUser(u extension.User) {
	// TODO implement me
	// panic("implement me")
}

func (e *defaultExtension) DeinitUser(u extension.User) {
	// TODO implement me
	panic("implement me")
}

func (e *defaultExtension) RunUser(u extension.User) {
	// TODO implement me
	panic("implement me")
}

func (e *defaultExtension) RunSpace(s extension.Space) {
	// TODO implement me
	panic("implement me")
}
