package create

import (
	"context"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
)

//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

type usecase interface {
	Create(ctx context.Context, in createuc.Input) (*trip.Trip, error)
}
