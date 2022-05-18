package utils

import "github.com/google/uuid"

func BinId(x uuid.UUID) []byte {
	binid, _ := x.MarshalBinary()
	return binid
}
