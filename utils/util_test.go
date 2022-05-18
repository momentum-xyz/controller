package utils

import (
	"fmt"
	"github.com/google/uuid"
	"testing"
)

func TestPlaceKindFromMap(t *testing.T) {
	uuidHelperParse := func(s interface{}) uuid.UUID {
		u, err := uuid.Parse(s.(string))
		if err != nil {
			_ = fmt.Errorf("error in parsing: %+v, %v", s, err)
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
			got := SpaceTypeFromMap(test.data)
			if got != test.want {
				t.Errorf("error in SpaceTypeFromMap. Got: %s, want: %s", got, test.want)
			}
		})
	}
}
