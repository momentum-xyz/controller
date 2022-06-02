package utils

import (
	"database/sql"

	"github.com/momentum-xyz/controller/internal/logger"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var log = logger.L()

func LoadRow(rows *sql.Rows) (map[string]interface{}, error) {
	if !rows.Next() {
		return nil, ErrNotFound // possible bug ?
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get columns")
	}
	count := len(columns)
	values := make([]interface{}, count)
	valuePtrs := make([]interface{}, count)

	for i := 0; i < count; i++ {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, errors.WithMessage(err, "failed to scan rows")
	}

	entry := make(map[string]interface{})
	for i, col := range columns {
		var v interface{}
		val := values[i]
		b, ok := val.([]byte)
		if ok {
			v = string(b)
		} else {
			v = val
		}
		entry[col] = v
	}
	return entry, nil
}

func GetFromAny[V any](val any, defaultValue V) V {
	if val == nil {
		return defaultValue
	}

	v, ok := val.(V)
	if ok {
		return v
	}

	return defaultValue
}

func GetFromAnyMap[K comparable, V any](amap map[K]any, key K, defaultValue V) V {
	if val, ok := amap[key]; ok {
		return GetFromAny(val, defaultValue)
	}
	return defaultValue
}

func SpaceTypeFromMap(parametersMap map[string]interface{}) (uuid.UUID, error) {
	rt, ok := parametersMap["kind"]
	if !ok {
		return uuid.Nil, errors.Errorf("kind not found")
	}
	var k string
	if k, ok = rt.(string); !ok || k == "default" {
		return uuid.Nil, errors.Errorf("kind is default")
	}
	kind, err := uuid.Parse(k)
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to parse kind")
	}
	return kind, nil
}

func DbToUuid(f interface{}) (uuid.UUID, error) {
	q, err := uuid.FromBytes([]byte(GetFromAny(f, "")))
	if err != nil {
		return uuid.Nil, errors.WithMessage(err, "failed to parse uuid")
	}
	return q, nil
}
