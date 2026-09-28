# Trip Service

HTTP-сервис для управления поездками.

## Требования

- Go 1.24+
- Docker Desktop
- tripgoctl

## Запуск

```bash
# Поднять окружение
tripgoctl cluster start
tripgoctl environment start

# Накатить миграции
make migrate

# Запустить сервис
make run
```

## Переменные окружения

| Переменная | Описание | По умолчанию |
|---|---|---|
| `HTTP_ADDR` | Адрес HTTP-сервера | `:8080` |
| `LOG_LEVEL` | Уровень логов | `info` |
| `SHUTDOWN_TIMEOUT` | Таймаут graceful shutdown | `10s` |
| `DATABASE_URL` | Адрес PostgreSQL | — |
| `DATABASE_MAX_CONNS` | Максимум соединений в пуле | `10` |
| `DATABASE_MIN_CONNS` | Минимум соединений в пуле | `2` |
| `DATABASE_MAX_CONN_LIFETIME` | Время жизни соединения | `30m` |
| `DATABASE_CONNECT_TIMEOUT` | Таймаут подключения к БД | `5s` |
| `DATABASE_QUERY_TIMEOUT` | Таймаут запроса к БД | `3s` |

## Решения

### Уровень изоляции транзакций

Используется `READ COMMITTED`. Этого достаточно для данного сервиса: каждый запрос читает только зафиксированные данные. Уникальный индекс `trips_driver_active_idx` защищает от двух активных поездок у одного водителя даже при параллельных запросах — база сама разруливает конфликт на уровне блокировок.

### Менеджер транзакций

Реализован в `internal/tx/tx.go`. Интерфейс:

```go
type TxManager interface {
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}
```

`Do` открывает транзакцию, кладёт её в контекст и вызывает `fn`. Если `fn` вернула `nil` — `COMMIT`, если ошибку или панику — `ROLLBACK`. Репозиторий достаёт транзакцию из контекста через `tx.GetExecutor`. Вложенный `Do` переиспользует существующую транзакцию.

### Запрет двух активных поездок

В миграции создан частичный уникальный индекс:

```sql
CREATE UNIQUE INDEX trips_driver_active_idx ON trips (driver_id) WHERE status = 'active';
```

Два параллельных `INSERT` с одним `driver_id` — один проходит, второй получает ошибку PostgreSQL `23505`, которая маппится в `409 driver_busy`.