package positions

import (
	"context"

	"github.com/google/uuid"
)

type pushGateway interface {
	RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error
}
