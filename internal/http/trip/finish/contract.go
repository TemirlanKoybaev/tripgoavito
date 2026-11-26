package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type usecase interface {
	Finish(ctx context.Context, id uuid.UUID) (*trip.Trip, error)
}
