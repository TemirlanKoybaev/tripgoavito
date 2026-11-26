package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type tripRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*trip.Trip, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (*trip.Trip, error)
	InsertPosition(ctx context.Context, position trip.Position) (trip.Position, error)
	UpdateLastPositionAt(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error
	ListPositions(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type positionMetrics interface {
	PositionSaved(ctx context.Context)
}
