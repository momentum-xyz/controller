package config

import "github.com/google/uuid"

type World struct {
	Kind       string               `json:"kind"`
	Spaces     map[string]uuid.UUID `json:"spaces"`
	SpaceTypes map[string]uuid.UUID `json:"space_types"`
	Effects    map[string]uuid.UUID `json:"effects"`
}
