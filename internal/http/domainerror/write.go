package domainerror

import (
	"encoding/json"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
)

func Write(w http.ResponseWriter, r *http.Request, problem api.Problem) error {
	instance := r.URL.Path
	problem.Instance = &instance

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(int(problem.Status))
	return json.NewEncoder(w).Encode(problem)
}
