package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OperationRecorder принимает измерения запросов к БД.
// Интерфейс объявлен здесь, у потребителя; реализует его telemetry.Metrics.
type OperationRecorder interface {
	RecordDBOperation(ctx context.Context, d time.Duration, operation, collection, errType string)
}

type startKey struct{}

type queryStart struct {
	at         time.Time
	operation  string
	collection string
}

// metricsTracer измеряет длительность каждого запроса и кладёт её в
// db.client.operation.duration с лейблами db.operation.name, db.collection.name
// и error.type. otelpgx этих лейблов в метрике не даёт, поэтому считаем сами.
type metricsTracer struct {
	recorder OperationRecorder
}

var collectionRe = regexp.MustCompile(`(?i)\b(?:from|into|update)\s+"?([a-z_][a-z0-9_.]*)"?`)

func (t metricsTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	start := queryStart{at: time.Now(), operation: operationName(data.SQL)}
	// Таблицу берём только у запросов с данными: у BEGIN/COMMIT её нет.
	if start.operation != "BEGIN" && start.operation != "COMMIT" && start.operation != "ROLLBACK" {
		if m := collectionRe.FindStringSubmatch(data.SQL); m != nil {
			start.collection = m[1]
		}
	}
	return context.WithValue(ctx, startKey{}, start)
}

func (t metricsTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(startKey{}).(queryStart)
	if !ok {
		return
	}
	t.recorder.RecordDBOperation(ctx, time.Since(start.at), start.operation, start.collection, errorType(data.Err))
}

// operationName — первое слово запроса в верхнем регистре (SELECT, INSERT, ...).
func operationName(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

// errorType: SQLSTATE для ошибок Postgres, иначе имя типа ошибки.
// Пустая строка значит «ошибки не было».
func errorType(err error) string {
	if err == nil {
		return ""
	}
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		return pgErr.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return fmt.Sprintf("%T", err)
	}
}
