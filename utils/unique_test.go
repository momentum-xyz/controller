package utils

import (
	"context"
	"testing"
)

func TestUnique_Equals(t *testing.T) {
	_, cancelFn := context.WithCancel(context.Background())
	tests := []struct {
		name string
		args any
		want bool
	}{
		{
			name: "Test1 - Cancel Func",
			args: cancelFn,
			want: false},
		{
			name: "Test2 - Primitive",
			args: 3,
			want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uq, uq2 := NewUnique(tt.args), NewUnique(tt.args)
			if got := uq.Equals(uq2); got != tt.want {
				t.Errorf("Unique.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}
