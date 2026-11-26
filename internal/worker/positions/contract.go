package positions

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type pushGateway interface {
	RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error
}
