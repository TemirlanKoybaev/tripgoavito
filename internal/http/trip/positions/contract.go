package positions

import (
	"context"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type usecase interface {
	Create(ctx context.Context, position trip.Position) (trip.Position, error)
	List(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error)
}
