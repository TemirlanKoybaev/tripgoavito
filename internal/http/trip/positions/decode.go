package positions

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

const maxBodyBytes = 1 << 20

func decode(w http.ResponseWriter, r *http.Request) (api.PositionData, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil {
		return api.PositionData{}, &trip.ValidationError{Message: "body must be a JSON object"}
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return api.PositionData{}, &trip.ValidationError{Message: "body must contain a single JSON object"}
	}
	if len(fields) != 2 || fields["coordinates"] == nil || fields["recorded_at"] == nil {
		return api.PositionData{}, &trip.ValidationError{Message: "body must contain coordinates and recorded_at"}
	}

	var coords map[string]json.RawMessage
	if err := json.Unmarshal(fields["coordinates"], &coords); err != nil || len(coords) != 2 {
		return api.PositionData{}, &trip.ValidationError{Message: "coordinates must contain latitude and longitude"}
	}
	var latitude, longitude *float64
	if err := json.Unmarshal(coords["latitude"], &latitude); err != nil || latitude == nil {
		return api.PositionData{}, &trip.ValidationError{Message: "coordinates.latitude must be a number"}
	}
	if err := json.Unmarshal(coords["longitude"], &longitude); err != nil || longitude == nil {
		return api.PositionData{}, &trip.ValidationError{Message: "coordinates.longitude must be a number"}
	}

	var recordedAt string
	if err := json.Unmarshal(fields["recorded_at"], &recordedAt); err != nil {
		return api.PositionData{}, &trip.ValidationError{Message: "recorded_at must be an RFC 3339 date-time"}
	}
	at, err := time.Parse(time.RFC3339, recordedAt)
	if err != nil {
		return api.PositionData{}, &trip.ValidationError{Message: "recorded_at must be an RFC 3339 date-time"}
	}
	return api.PositionData{Coordinates: api.Coordinates{Latitude: *latitude, Longitude: *longitude}, RecordedAt: at}, nil
}
