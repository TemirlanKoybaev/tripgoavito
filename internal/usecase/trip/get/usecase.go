package get

import (
	"context"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type UseCase struct {
	trips tripRepository
}

func New(trips tripRepository) *UseCase {
	return &UseCase{trips: trips}
}

func (u *UseCase) Get(ctx context.Context, id uuid.UUID) (*trip.Trip, error) {
	return u.trips.GetByID(ctx, id)
}
