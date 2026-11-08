# Trip Service

HTTP-сервис для управления поездками.

## Требования

- Go 1.26+
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
| `HTTP_READ_TIMEOUT` | Таймаут чтения запроса целиком | `5s` |
| `HTTP_READ_HEADER_TIMEOUT` | Таймаут чтения заголовков | `2s` |
| `HTTP_WRITE_TIMEOUT` | Таймаут записи ответа | `10s` |
| `HTTP_IDLE_TIMEOUT` | Таймаут простаивающего keep-alive соединения | `60s` |
| `DATABASE_URL` | Адрес PostgreSQL | — |
| `DATABASE_MAX_CONNS` | Максимум соединений в пуле | `10` |
| `DATABASE_MIN_CONNS` | Минимум соединений в пуле | `2` |
| `DATABASE_MAX_CONN_LIFETIME` | Время жизни соединения | `30m` |
| `DATABASE_CONNECT_TIMEOUT` | Таймаут подключения к БД | `5s` |
| `DATABASE_QUERY_TIMEOUT` | Таймаут запроса к БД (и `Ping` в `/ready`) | `3s` |
| `OTEL_SERVICE_NAME` | Имя сервиса в телеметрии и поле `service` в логах | `trip-service` |
| `SERVICE_VERSION` | Версия сборки, атрибут ресурса `service.version` | `dev` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Адрес OTLP/HTTP-коллектора (читает OTel SDK, см. `.env.example`) | по умолчанию SDK |

Все длительности должны быть больше нуля, `DATABASE_MIN_CONNS` не может превышать `DATABASE_MAX_CONNS` — иначе сервис не стартует.

## Архитектура

### Слои и зависимости

Стрелка означает «импортирует». Список взят из `go list`, а не из головы: правила для домена и usecase ещё и проверяет линтер (`depguard` в `.golangci.yml`, `make lint`).

```
cmd/trip-service ──► container ──► всё остальное (единственное место, где создаются реализации)

http/trip/{create,get,finish}, http/health ──► usecase (create: только тип Input), domain/trip,
                                               http/domainerror, http/response, api
http/router     ──► api, domain/trip, http/middleware, http/domainerror
usecase/trip/*  ──► domain/trip
repository/*    ──► domain/trip (trip), infra/postgres/transaction
infra/*         ──► ничего из internal
domain/trip     ──► ничего из internal
```

| Слой | Пакет | Что делает |
|---|---|---|
| домен | `internal/domain/trip` | сущность `Trip`, статусы, переход `Finish`, ошибки `ErrTripNotFound`, `ErrTripCompleted`, `ErrDriverBusy`, `ValidationError` |
| usecase | `internal/usecase/trip/{create,get,finish}` | сценарий: транзакция, вызовы репозиториев, после commit счётчик и бизнес-лог |
| транспорт | `internal/http/...` | разбор запроса, вызов usecase, ответ; `domainerror` переводит ошибку в `problem+json`; `middleware` |
| репозиторий | `internal/repository/{trip,history}` | SQL через squirrel и pgx, ошибки БД превращаются в доменные |
| инфраструктура | `internal/infra/{postgres,logger,telemetry}` | пул, менеджер транзакций, slog, OpenTelemetry |
| сборка | `internal/container`, `cmd/trip-service` | `main` читает конфиг, создаёт логгер, зовёт `container.New`, ждёт сигнала |

Цепочка middleware (`container.New`): `Recover → Tracing → Logging → Metrics → router`. Внутри роутера на каждой ручке стоит `TagRoute`: он узнаёт шаблон маршрута (`/api/v1/trips/{tripId}`) и отдаёт его спану, логу и метрикам.

Глобальных объектов в своём коде нет, всё собирается в `container.New`. Исключение — глобальные регистрации самого OpenTelemetry (`otel.SetTracerProvider`, `SetMeterProvider`, `SetErrorHandler`): `otelhttp` и `otelpgx` читают их оттуда.

### Где объявлены интерфейсы и почему

**Интерфейс объявляет тот, кто его использует**, в `contract.go` своего пакета, неэкспортируемым. Реализация про интерфейс не знает.

| Потребитель | Интерфейсы | Кто реализует |
|---|---|---|
| `usecase/trip/create/contract.go` | `tripRepository.Create`, `historyRepository.Create`, `txManager.Do`, `tripMetrics.TripCreated` | `repository/trip`, `repository/history`, `transaction`, `telemetry.Metrics` |
| `usecase/trip/finish/contract.go` | `tripRepository.GetForUpdate/Update`, `historyRepository.Create`, `txManager.Do`, `tripMetrics.TripCompleted` | они же |
| `usecase/trip/get/contract.go` | `tripRepository.GetByID` | `repository/trip` |
| `http/trip/{create,get,finish}/contract.go` | `usecase` с одним методом | `usecase/trip/*` |
| `http/router/router.go` | `createHandler`, `getHandler`, `finishHandler`, `healthHandler` | пакеты хендлеров |
| `http/middleware/metrics.go` | `httpMetrics` | `telemetry.Metrics` |
| `infra/postgres/metrics_tracer.go` | `OperationRecorder` | `telemetry.Metrics` |
| `infra/telemetry/metrics.go` | `ActiveTripsSource.CountActive` | `repository/trip` |

Почему так:

1. **Направление зависимостей.** usecase не импортирует ни репозиторий, ни телеметрию, ни `net/http`; в `go list -deps ./internal/usecase/... ./internal/domain/...` нет `net/http`, `pgx`, `chi`, `otel`. Репозиторий и `telemetry.Metrics` подходят под интерфейсы неявно, без импорта usecase.
2. **Узкие интерфейсы.** usecase `get` видит у репозитория один метод `GetByID`, а не весь `Repository`. Поэтому моки маленькие, а тест проверяет ровно то, что сценарий вызывает.
3. **Тесты без БД.** Моки генерируются `go:generate mockgen` в `mocks_test.go` рядом с кодом. Unit-тест usecase проверяет оркестрацию (что вызвано, в каком порядке, что при ошибке счётчик не растёт) за миллисекунды и без PostgreSQL.
4. **Второй транспорт не трогает бизнес-логику.** usecase не знает, кто его вызывает: HTTP-хендлер, а в работе 4 обработчик сообщения брокера. Для второго транспорта добавляется новый слой обработчиков, который вызывает те же usecase; `domain` и `usecase` не меняются. Это следствие устройства кода, самого consumer в репозитории пока нет.

Что в эту схему не укладывается:
- `*slog.Logger` внедряется в usecase как конкретный тип: это стандартный логгер, интерфейс над ним ничего не добавляет.
- `repository/trip` держит `*pgxpool.Pool` напрямую и без `contract.go`; его проверяют интеграционные тесты на настоящей БД.
- Пакет `transaction` экспортирует свой `TxManager` как тип результата `New` (его использует интеграционный тест); usecase от него не зависят, у каждого свой `txManager`.

### Ошибки

Доменные ошибки объявлены в `domain/trip/errors.go`. Перевод «ошибка → статус и `problem+json`» один: `domainerror.Problem` (через `errors.Is/As`). Репозиторий превращает нарушение уникального индекса (`23505`) в `ErrDriverBusy`, `pgx.ErrNoRows` в `ErrTripNotFound`. Любая неизвестная ошибка отдаётся клиенту как `500 internal_error` без деталей. Настоящая причина кладётся в слот на контексте запроса (`domainerror.Slot`) и попадает в запись «request handled» (поле `error`, уровень Error только для 5xx) и событием в корневой спан.

### Транзакции и побочные эффекты

usecase открывает транзакцию через `txManager.Do`, репозитории берут исполнителя из контекста (`transaction.GetExecutor`). Счётчики `tripgo.trips.created/completed` и логи «trip created/completed» пишутся только после успешного commit и не внутри транзакции.

### Телеметрия

| Сигнал | Где создаётся |
|---|---|
| `http.server.request.duration`, `http.server.active_requests` | `middleware.Metrics` (`http.route` — шаблон; встроенные метрики `otelhttp` отключены) |
| `db.client.operation.duration` (`db.operation.name`, `db.collection.name`, `error.type`) | `infra/postgres/metrics_tracer.go`, рядом с `otelpgx` через `pgx/multitracer` |
| `tripgo.trips.created`, `tripgo.trips.completed` | usecase `create` и `finish` после commit |
| `tripgo.trips.active` | observable gauge, значение читает `repository/trip.CountActive` в момент сбора |
| трейсы | `otelhttp` (корневой спан `METHOD /шаблон`), `otelpgx` (SQL-спаны); `error.type` на любой ошибке, статус Error только на 5xx и ошибках БД |
| логи | `slog` JSON; `trace_id` и `span_id` добавляет `logger.traceHandler`, он читает спан из `ctx`, поэтому логировать нужно с `*Context` |

Ошибки самого OTel SDK (например, коллектор недоступен) пишутся в лог как Warn «telemetry error» не чаще раза в 30 секунд (`telemetry.ErrorHandler`); сервис при этом продолжает отвечать.

### Тесты

- **Unit** (`make test`, с `-race` и покрытием): домен, usecase (gomock), хендлеры, `domainerror`, middleware (Recover, Logging, Metrics, Tracing), трейсеры БД.
- **Интеграционные** (`make test-integration`, тег `integration`): репозиторий на настоящем PostgreSQL, включая откат транзакции, конфликт «водитель занят» и `CountActive`. База общая, поэтому тесты чистят за собой свои строки и сравнивают счётчики с исходным значением.

## Отступления от ТЗ §4.1

| Отступление | Причина |
|---|---|
| `cmd/trip-service` вместо `cmd/service` | Каталог появился в лабе 1 (коммит `757fb73`); на него завязаны `make run` и README. Переименование ничего не даёт. |
| `api/` вместо `internal/generated` | Код генерируется `make generate` в `api/api.gen.go` (пакет `api`) с лабы 1 (`07a5773`); линтер исключает `api/` из проверки. Перенос потребовал бы поменять импорты во всех хендлерах без выгоды. |
| Нет `internal/model` | Сущность `trip.Trip` лежит в `domain/trip`, без тегов и инфраструктуры, и её напрямую используют usecase, репозиторий и транспорт. Отдельный `model` дал бы копию структуры и конвертеры туда-обратно. Структуры границ свои: `api.*` для транспорта и `create.Input` для входа usecase. |
| Лишние пакеты `http/router`, `http/response`, `http/health` | `router` реализует сгенерированный `api.ServerInterface` и раздаёт вызовы хендлерам; `response` общий для трёх ручек (запись JSON и `trip.Trip → api.Trip`), вместо `convert.go` в каждой; `health` (`/health`, `/ready`) не про поездки, поэтому не лежит в `http/trip/`. В `http/trip/create` вместо `convert.go` есть `validate.go`: он строго разбирает и проверяет тело запроса. |
| Лишний `repository/history` и нет `contract.go`, `convert.go` в `repository/trip` | Вторая таблица `trip_status_history` получила свой репозиторий. Репозиторий работает с `*pgxpool.Pool` и сканирует строки прямо в `domain.Trip`, поэтому интерфейс и конвертер ему не нужны; он проверяется интеграционными тестами, а не моками. |
| `infra/logger` отдельно от `infra/telemetry` | Логгер нужен `main` до создания контейнера: уровень берётся из конфига, а ошибку самого конфига тоже надо залогировать. Телеметрия поднимается позже, внутри `container.New`, и может не подняться. У пакетов разный жизненный цикл, и `logger` не тянет за собой SDK OpenTelemetry. |
| `make test` запускает только пакеты с тестами | В этом окружении `go test -cover` на пакете без тестовых файлов падает: нет инструмента `covdata`. Список пакетов берётся из `go list` по `TestGoFiles`/`XTestGoFiles`. |
| Имя таблицы в `db.collection.name` берётся регулярным выражением по тексту SQL | `pgx` отдаёт трейсеру только строку запроса, а `otelpgx` это поле для обычных запросов не заполняет. Регэксп (`FROM`/`INTO`/`UPDATE <имя>`) корректен для запросов репозиториев, они простые; для `JOIN`, подзапросов и CTE он взял бы только первую таблицу. Кардинальность ограничена: SQL пишем мы, а `BEGIN/COMMIT/ROLLBACK` таблицы не получают. |

## Команды

| Команда | Что делает |
|---|---|
| `make test` | unit-тесты с `-race` и покрытием |
| `make test-integration` | интеграционные тесты репозитория; нужны `.env` с `DATABASE_URL`, `tripgoctl environment start` и накатанные миграции |
| `make lint` | `golangci-lint run ./...`; если команда не найдена: `export PATH=$PATH:$(go env GOPATH)/bin` |
| `make run` | запуск сервиса (`.env` подхватывается автоматически) |

## Решения

### Уровень изоляции транзакций

Используется `READ COMMITTED`. Этого достаточно для данного сервиса: каждый запрос читает только зафиксированные данные. Уникальный индекс `trips_driver_active_idx` защищает от двух активных поездок у одного водителя даже при параллельных запросах — база сама разруливает конфликт на уровне блокировок.

### Менеджер транзакций

Реализован в `internal/infra/postgres/transaction/transaction.go`. Интерфейс:

```go
type TxManager interface {
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}
```

`Do` открывает транзакцию, кладёт её в контекст и вызывает `fn`. Если `fn` вернула `nil` — `COMMIT`, если ошибку или панику — `ROLLBACK`. Репозиторий достаёт исполнителя из контекста через `transaction.GetExecutor`: есть транзакция — работает через неё, нет — через пул. Вложенный `Do` переиспользует существующую транзакцию. При откате используется контекст без отмены (клиент мог оборвать запрос), но с собственным таймаутом.

### Ошибки базы данных

Репозиторий превращает ошибки PostgreSQL в доменные (`trip.ErrTripNotFound`, `trip.ErrDriverBusy`), хендлеры сравнивают их через `errors.Is` и про `pgx` ничего не знают.

### Завершение поездки

`finish` выполняется одной транзакцией: `UPDATE ... WHERE status = 'active'`, запись `active → completed` в `trip_status_history` и чтение результата. Условие `status = 'active'` прямо в `UPDATE` защищает от гонки: при двух одновременных запросах второй дожидается блокировки строки, не находит активной поездки (0 строк) и получает `409 trip_completed`, `finished_at` не перезаписывается. Если затронуто 0 строк, дополнительным чтением различаются `404` и `409`.

### Валидация запроса

Тело `POST /trips` разбирается строго: неизвестные поля, отсутствующие обязательные поля и координаты вне диапазонов дают `400 invalid_request`. Невалидный `tripId` тоже отдаётся как `problem+json`.

### Время

Время в ответах — в UTC, с точностью до микросекунд (так его хранит PostgreSQL), поэтому ответы `POST` и `GET` совпадают.

### Запрет двух активных поездок

В миграции создан частичный уникальный индекс:

```sql
CREATE UNIQUE INDEX trips_driver_active_idx ON trips (driver_id) WHERE status = 'active';
```

Два параллельных `INSERT` с одним `driver_id` — один проходит, второй получает ошибку PostgreSQL `23505`, которая маппится в `409 driver_busy`.