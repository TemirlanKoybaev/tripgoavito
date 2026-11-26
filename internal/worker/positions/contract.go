package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type pushGateway interface {
	RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error
}

type poolMetrics interface {
	PositionRequest(ctx context.Context, result string)
	RecordPositionRequestDuration(ctx context.Context, duration time.Duration, result string)
}

type staleTripRepository interface {
	ListStaleTrips(ctx context.Context, cutoff time.Time, after *trip.StaleTripCursor, limit int) ([]trip.StaleTrip, error)
}

type scannerMetrics interface {
	StaleCheck(ctx context.Context, result string)
	RecordStaleRows(ctx context.Context, rows int64)
}

type scanTicker interface {
	Ticks() <-chan time.Time
	Stop()
}
