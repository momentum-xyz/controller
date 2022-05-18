package space

import (
	"database/sql"
	"github.com/google/uuid"
)

type childrenSpace struct {
	id           uuid.UUID `db:"id"`
	SpaceVisible byte      `db:"space_visible"`
	TypeVisible  byte      `db:"type_visible"`
	Position     string    `db:"position"`
}

type spaceAttribute struct {
	Name  string         `db:"name"`
	Flag  int64          `db:"flag"`
	Value sql.NullString `db:"value"`
}
