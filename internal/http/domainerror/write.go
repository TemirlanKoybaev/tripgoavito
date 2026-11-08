package domainerror

import (
	"encoding/json"
	"net/http"
)

// Write отвечает клиенту problem+json. Настоящая причина 5xx и сбой записи
// ответа не логируются здесь, а попадают в слот и уходят в «request handled».
func Write(w http.ResponseWriter, r *http.Request, err error) {
	status, problem := Problem(err)
	if status == http.StatusInternalServerError {
		Record(r.Context(), err)
	}

	instance := r.URL.Path
	problem.Instance = &instance

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if encErr := json.NewEncoder(w).Encode(problem); encErr != nil {
		Record(r.Context(), encErr)
	}
}
