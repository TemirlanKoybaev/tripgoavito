package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

func decodeTripData(w http.ResponseWriter, r *http.Request) (api.TripData, error) {
	var body api.TripData

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return body, errors.New("cannot read request body")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return body, errors.New("body must be a JSON object")
	}
	for _, key := range []string{"user_id", "driver_id", "start_point", "end_point", "price"} {
		if isMissing(raw[key]) {
			return body, fmt.Errorf("%s is required", key)
		}
	}
	for _, key := range []string{"start_point", "end_point"} {
		var point map[string]json.RawMessage
		if err := json.Unmarshal(raw[key], &point); err != nil {
			return body, fmt.Errorf("%s must be an object", key)
		}
		for _, field := range []string{"latitude", "longitude"} {
			if isMissing(point[field]) {
				return body, fmt.Errorf("%s.%s is required", key, field)
			}
		}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return body, fmt.Errorf("invalid body: %v", err)
	}

	if body.UserId == uuid.Nil || body.DriverId == uuid.Nil {
		return body, errors.New("user_id and driver_id must be non-empty UUIDs")
	}
	if body.Price < 0 {
		return body, errors.New("price must be >= 0")
	}
	if err := validatePoint("start_point", body.StartPoint); err != nil {
		return body, err
	}
	if err := validatePoint("end_point", body.EndPoint); err != nil {
		return body, err
	}

	return body, nil
}

func isMissing(v json.RawMessage) bool {
	return len(v) == 0 || bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

func validatePoint(name string, c api.Coordinates) error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return fmt.Errorf("%s.latitude must be between -90 and 90", name)
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return fmt.Errorf("%s.longitude must be between -180 and 180", name)
	}
	return nil
}
