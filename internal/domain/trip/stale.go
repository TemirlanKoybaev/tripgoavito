package trip

import (
	"time"

	"github.com/google/uuid"
)

type StaleTrip struct {
	TripID      uuid.UUID
	DriverID    uuid.UUID
	EffectiveAt time.Time // Время последней координаты или начала поездки.
}

type StaleTripCursor struct {
	EffectiveAt time.Time
	TripID      uuid.UUID
}
