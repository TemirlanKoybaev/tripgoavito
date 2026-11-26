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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

// metricsTracer измеряет длительность каждого запроса, кладёт её в
// db.client.operation.duration с лейблами db.operation.name, db.collection.name
// и error.type (otelpgx этих лейблов в метрике не даёт, поэтому считаем сами)
// и добавляет error.type на спан запроса.
type metricsTracer struct {
	recorder OperationRecorder
}

var collectionRe = regexp.MustCompile(`(?i)\b(?:from|into|update)\s+"?([a-z_][a-z0-9_.]*)"?`)

func (t metricsTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	start := queryStart{at: time.Now(), operation: operationName(data.SQL), collection: collectionName(data.SQL)}
	return context.WithValue(ctx, startKey{}, start)
}

func (t metricsTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(startKey{}).(queryStart)
	if !ok {
		return
	}
	errType := errorType(data.Err)
	t.recorder.RecordDBOperation(ctx, time.Since(start.at), start.operation, start.collection, errType)

	// Спан запроса создал otelpgx (он ставит codes.Error и записывает ошибку), но
	// db.collection.name для обычных запросов и error.type не размечает. Трейсер
	// стоит в multitracer первым, поэтому до span.End у otelpgx успеваем их добавить.
	markSpan(trace.SpanFromContext(ctx), start.collection, errType)
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

// collectionName — таблица запроса. У BEGIN/COMMIT/ROLLBACK её нет.
func collectionName(sql string) string {
	switch operationName(sql) {
	case "BEGIN", "COMMIT", "ROLLBACK":
		return ""
	}
	if m := collectionRe.FindStringSubmatch(sql); m != nil {
		return m[1]
	}
	return ""
}

type prepareKey struct{}

// TracePrepareStart / TracePrepareEnd: prepare тоже порождает спан у otelpgx, и
// ему нужны те же атрибуты. В метрику prepare не пишем: это не операция приложения.
func (t metricsTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	return context.WithValue(ctx, prepareKey{}, data.SQL)
}

func (t metricsTracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	sql, _ := ctx.Value(prepareKey{}).(string)
	markSpan(trace.SpanFromContext(ctx), collectionName(sql), errorType(data.Err))
}

// markSpan добавляет на спан запроса атрибуты, которых нет у otelpgx.
func markSpan(span trace.Span, collection, errType string) {
	if collection != "" {
		span.SetAttributes(attribute.String("db.collection.name", collection))
	}
	if errType != "" {
		span.SetAttributes(attribute.String("error.type", errType))
	}
}
