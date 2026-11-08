package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type (
	tripRepository interface {
		Create(ctx context.Context, t *trip.Trip) error
	}

	historyRepository interface {
		Create(ctx context.Context, tripID uuid.UUID, from *string, to string) error
	}

	tripMetrics interface {
		TripCreated(ctx context.Context)
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}
)
