package utils

import (
	"database/sql"
	"github.com/google/uuid"

	"github.com/momentum-xyz/controller/internal/logger"
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

func F64FromMap(parametersMap map[string]interface{}, k string, defaultValue float64) float64 {
	if v1, ok := parametersMap[k]; ok {
		return v1.(float64)
	}

	return defaultValue
}

func BoolFromMap(parametersMap map[string]interface{}, k string, defaultValue bool) bool {
	if v1, ok := parametersMap[k]; ok {
		return v1.(bool)
	}

	return defaultValue
}

func SpaceTypeFromMap(parametersMap map[string]interface{}) uuid.UUID {
	rt, ok := parametersMap["kind"]
	if !ok || rt.(string) == "default" {
		return uuid.Nil
	}
	kind, err := uuid.Parse(rt.(string))
	if err != nil {
		return uuid.Nil
	}
	return kind
}

func DbToUuid(f interface{}) uuid.UUID {
	q, _ := uuid.FromBytes([]byte((f).(string)))
	return q
}
