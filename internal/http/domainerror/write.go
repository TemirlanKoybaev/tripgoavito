package domainerror

import (
	"encoding/json"
	"log"
	"net/http"
)

func Write(w http.ResponseWriter, r *http.Request, err error) {
	status, problem := Problem(err)
	if status == http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
	}

	instance := r.URL.Path
	problem.Instance = &instance

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(problem)
}
