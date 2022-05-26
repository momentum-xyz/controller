package utils

import (
	"database/sql"
	"github.com/momentum-xyz/controller/internal/logger"

	"github.com/google/uuid"
)

var log = logger.L()

func LoadRow(rows *sql.Rows) map[string]interface{} {
	if !rows.Next() {
		return nil // possible bug ?
	}
	columns, err := rows.Columns()
	if err != nil {
		log.Errorf("failed to load row: %+v", err)
		return nil
	}
	count := len(columns)
	values := make([]interface{}, count)
	valuePtrs := make([]interface{}, count)

	for i := 0; i < count; i++ {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		log.Errorf("failed to scan row: %+v", err)
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
	return entry
}

func F64FromMap(parametersMap map[string]any, k string, defaultValue float64) float64 {
	if v, ok := parametersMap[k]; ok {
		if v1, ok := v.(float64); ok {
			return v1
		}
	}

	return defaultValue
}

func BoolFromMap(parametersMap map[string]any, k string, defaultValue bool) bool {
	if v, ok := parametersMap[k]; ok {
		if v1, ok := v.(bool); ok {
			return v1
		}
	}

	return defaultValue
}

func SpaceTypeFromMap(parametersMap map[string]interface{}) uuid.UUID {
	rt, ok := parametersMap["kind"]
	if !ok {
		log.Warn("SpaceTypeFromMap: kind not found")
		return uuid.Nil
	}
	var k string
	if k, ok = rt.(string); !ok || k == "default" {
		log.Warn("SpaceTypeFromMap: kind is default")
		return uuid.Nil
	}
	kind, err := uuid.Parse(k)
	if err != nil {
		log.Warnf("SpaceTypeFromMap: kind is not a valid uuid: %s\n", k)
		return uuid.Nil
	}
	return kind
}

func DbToUuid(f interface{}) uuid.UUID {
	q, err := uuid.FromBytes([]byte((f).(string)))
	if err != nil {
		log.Errorf("DbToUuid: %+v", err)
	}
	return q
}
