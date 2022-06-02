package utils

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func TestF64FromMap(t *testing.T) {
	m := map[string]interface{}{
		"a": 1.0,
		"b": "2",
		"c": "3",
	}
	def := 4.44
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		default_ float64
		expected float64
	}{
		{
			input:    m,
			key:      "a",
			expected: 1.0,
		},
		{
			input:    m,
			key:      "b",
			expected: def,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := GetFromAnyMap(test.input, test.key, def)
			if actual != test.expected {
				t.Errorf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}

func TestPlaceKindFromMap(t *testing.T) {
	uuidHelperParse := func(s interface{}) uuid.UUID {
		u, err := uuid.Parse(s.(string))
		if err != nil {
			_ = fmt.Errorf("error in parsing: %s, %v", s, err)
		}

		return u
	}
	tt := []struct {
		name string
		data map[string]interface{}
		want uuid.UUID
	}{
		{
			name: "Test: getting successful kind from map",
			data: map[string]interface{}{
				"kind": "86229140-93a5-4206-ab3b-75713c38f6a6",
			},
			want: uuidHelperParse("86229140-93a5-4206-ab3b-75713c38f6a6"),
		},
		{
			name: "Test: getting from empty map",
			data: map[string]interface{}{},
			want: uuid.Nil,
		},
		{
			name: "Test: getting nil kind from map",
			data: map[string]interface{}{
				"kind": "default",
			},
			want: uuid.Nil,
		},
	}
	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			got, _ := SpaceTypeFromMap(test.data)
			if got != test.want {
				t.Errorf("error in SpaceTypeFromMap. Got: %s, want: %s", got, test.want)
			}
		})
	}
}
