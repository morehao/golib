package task

import "github.com/google/uuid"

func GenRunID() string {
	return uuid.Must(uuid.NewV7()).String()
}
