# Simple-VPN-Builder

> **Распределенная мног узловая VPN control plane**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org/)
[![Build Status](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/Security-Policy-green.svg)](SECURITY.md)
[![Telegram Chat](https://img.shields.io/badge/Telegram-Chat-26A5E4?logo=telegram)](https://t.me/ivanchikbyte)
[![Telegram Channel](https://img.shields.io/badge/Telegram-Channel_RU-26A5E4?logo=telegram)](https://t.me/ivanchik_byte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?logo=gmail)](mailto:ivanchikbyte@gmail.com)

| [English version](README.md) | [Сообщить о проблеме](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues) |

Simple-VPN-Builder — распределенная мультитенантная платформа оркестрации VPN для коммерческих VPN-провайдеров, корпоративных overlay-сетей и обхода цензуры. Центральная Go control plane управляет легковесными автономными агентами на удаленных выходных серверах.

## Зачем этот проект

Односерверные панели (3X-UI, Marzban, голый WireGuard) упираются в потолок: одна машина, одна точка отказа, ручная работа на каждой ноде. Simple-VPN-Builder идет с противоположного конца — control plane отделена от выходных нод с первого дня, поэтому мощность растет добавлением дешевых серверов в новых регионах, а не ресайзом одного бокса. Ноды автономны: при обрыве связи с control plane установленные туннели продолжают пропускать трафик, а агент сам пересинкается, когда стрим вернется.

## Содержание

- [Что это делает](#что-это-делает)
- [Архитектура](#архитектура)
- [Технологии](#технологии)
- [Требования](#требования)
- [Быстрый старт](#быстрый-старт)
- [Использование](#использование)
- [Конфигурация](#конфигурация)
- [Эндпоинты и порты](#эндпоинты-и-порты)
- [Структура проекта](#структура-проекта)
- [Доступные команды](#доступные-команды)
- [Тестирование](#тестирование)
- [Деплой](#деплой)
- [Документация](#документация)
- [Автор и контакты](#автор-и-контакты)
- [Участие в разработке](#участие-в-разработке)
- [Лицензия](#лицензия)

## Что это делает

- **Control plane**: REST API, админ-консоль, доставка подписок, биллинг, Telegram CRM-бот и gRPC-хаб, который пушит дельты конфигурации на ноды по двунаправленным mTLS-стримам.
- **Агент ноды**: применяет состояние WireGuard / AmneziaWG через netlink, управляет Xray VLESS-Reality инбаундами, настраивает NAT в nftables, отчитывается о здоровье и телеметрии.
- **Доставка подписок**: персональные токен-URL (`/sub/{token}`) с автоопределением клиента для Sing-box, Clash Meta (Mihomo), официального WireGuard, AmneziaVPN и base64-бандлов.
- **Админ-консоль**: серверный дашборд (HTMX + Alpine.js): ноды, пользователи, тарифы, креды, биллинг, аудит-лог, рассылки.
- **Telegram-бот**: сбор лидов, выдача триалов, платежи и управление аккаунтом поверх того же API.
- **Надежность**: метрики Prometheus, трейсинг OpenTelemetry, Redis sliding-window rate limiting, скрипты бэкапа/восстановления БД.
- **Поставка**: мультиарх бинари, пакеты `.deb`/`.rpm`, SBOM, подписи Cosign, Docker-образы, systemd-юниты, однострочный установщик.

## Архитектура

```
+---------------------------------------------------------------------------------+
|                                 CONTROL PLANE                                   |
|                                                                                 |
|   +-------------------+   +--------------------+   +------------------------+   |
|   |   REST API v1     |   |   Admin Web UI     |   |  Subscription Delivery |   |
|   |  (:8110 /api/v1)  |   |  (:8110 /admin)   |   |   (:8110 /sub/{token}) |   |
|   +---------+---------+   +---------+----------+   +-----------+------------+   |
|             |                       |                          |                |
|             +-----------------------+--------------------------+                |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |  Service Domain    |                                |
|                           | (Бизнес-логика)    |                                |
|                           +----+----------+----+                                |
|                                |          |                                     |
|               +----------------v---+  +---v----------------+                    |
|               |  PostgreSQL 16     |  |   Redis 7 Cache    |                    |
|               |  (Хранилище)       |  | (Лимиты / Токены)  |                    |
|               +--------------------+  +--------------------+                    |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |   gRPC Agent Hub   |                                |
|                           |   (:9090 with mTLS)|                                |
|                           +---------+----------+                                |
+-------------------------------------|-------------------------------------------+
                                       |
                        Bidirectional gRPC Streaming
                        (mTLS + Token Bucket Limit)
                                       |
          +----------------------------+----------------------------+
          |                                                         |
+--------v---------------------------------+     +-----------------v-----------------------+
|          NODE AGENT (Region 1)           |     |         NODE AGENT (Region 2)           |
|                                          |     |                                         |
| +--------------------------------------+ |     | +-------------------------------------+ |
| |        gRPC Sync & Heartbeat         | |     | |        gRPC Sync & Heartbeat        | |
| +-------------------+------------------+ |     | +-------------------+-----------------+ |
|                     |                    |     |                     |                 |
|      +--------------+-------------+      |     |      +--------------+-------------+   |
|      |                            |      |     |      |                            |   |
| +----v-------------+     +--------v----+ |     | +----v-------------+     +--------v-+ |
| | WireGuard/AWG    |     | Xray VLESS  | |     | | WireGuard/AWG    |     | Xray     | |
| | Netlink Engine   |     | Reality Core| |     | | Netlink Engine   |     | Reality  | |
| +----+-------------+     +--------+----+ |     | +----+-------------+     +--------+-+ |
|      |                            |      |     |      |                            |   |
| +----v----------------------------v----+ |     | +----v----------------------------v-+ |
| |        nftables Firewall & NAT       | |     | |        nftables Firewall & NAT      | |
| +--------------------------------------+ |     | +-------------------------------------+ |
+------------------------------------------+     +-----------------------------------------+
```

## Технологии

- **Язык**: Go 1.25
- **HTTP**: роутер Chi, админка на `html/template`, HTMX + Alpine.js
- **Данные**: PostgreSQL 16 (pgx, golang-migrate, sqlc), Redis 7 (лимиты, отзыв токенов)
- **Связь с нодами**: gRPC двунаправленные стримы с mTLS, Protobuf (`proto/agent/v1`)
- **VPN-ядро**: WireGuard / AmneziaWG через netlink, Xray-core VLESS-Reality, nftables
- **Аутентификация**: JWT access/refresh, API-ключи, TOTP 2FA, HMAC CSRF для веб-форм
- **Наблюдаемость**: метрики Prometheus, трейсинг OpenTelemetry
- **Релизы**: GoReleaser, Docker, systemd, Helm-чарт, cloud-init шаблоны

## Требования

- Go 1.25+
- Docker и Docker Compose v2 (для локального стенда)
- `buf`, `sqlc`, `oapi-codegen`, `golangci-lint`, `migrate` (ставятся через `make install-tools`)
- Linux с модулем ядра WireGuard для выходной ноды

## Быстрый старт

### Однострочная установка (сервер)

Control plane плюс агент на любой современный Linux-сервер:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- --all
```

Только выходная нода:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- \
  --agent \
  --cp-url cp.vpn.example.com:9090
```

### Диагностика ноды

```bash
vpnbuilder-agent doctor
vpnbuilder-agent doctor --json
```

### Локальная разработка

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder

# Инструменты разработчика (buf, sqlc, oapi-codegen, golangci-lint, migrate, air)
make install-tools

# PostgreSQL 16 + Redis 7
make dev-up

# Сборка всех бинарей
make build

# Тесты с race detector
make test

# Control plane с live reload
make run-cp
```

При первом старте на пустой базе control plane создает учетную запись `owner` (`admin@vpnbuilder.local`). Сразу смените пароль после первого входа. Сид-аккаунт создается только на свежей установке и никогда не сбрасывается при рестарте.

## Использование

Получить конфиг подписки (работает в браузере, Sing-box, Clash Meta, AmneziaVPN):

```bash
curl -H "User-Agent: sing-box" http://localhost:8110/sub/<user-token>
```

Войти в API и вывести список нод:

```bash
TOKEN=$(curl -s -X POST http://localhost:8110/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@vpnbuilder.local","password":"changeme"}' | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

curl -H "Authorization: Bearer $TOKEN" http://localhost:8110/api/v1/nodes/
```

Проверить здоровье ноды и забрать метрики:

```bash
vpnbuilder-agent doctor --json
curl -H "Authorization: Bearer $VPNBUILDER_METRICS_TOKEN" http://node:8081/metrics
```

## Конфигурация

Конфигурация — YAML плюс переменные окружения с префиксом `VPNBUILDER_` (точки превращаются в подчеркивания, например `VPNBUILDER_AUTH_JWT_SECRET`).

| Переменная | Описание | Пример |
| :--- | :--- | :--- |
| `VPNBUILDER_SERVER_HTTP_ADDR` | Адрес REST/админки | `:8110` |
| `VPNBUILDER_SERVER_GRPC_ADDR` | Адрес gRPC-хаба | `:9090` |
| `VPNBUILDER_DATABASE_DSN` | Строка подключения PostgreSQL | `postgres://user:pass@localhost:5432/vpnbuilder` |
| `VPNBUILDER_REDIS_ADDR` | Адрес Redis | `localhost:6379` |
| `VPNBUILDER_AUTH_JWT_SECRET` | Секрет подписи JWT, минимум 32 байта (обязателен) | вывод `openssl rand -hex 32` |
| `VPNBUILDER_SERVER_CORS_ALLOWED_ORIGINS` | Разрешенные CORS-источники | `https://admin.example.com` |
| `VPNBUILDER_METRICS_TOKEN` | Bearer-токен для `:8081/metrics` агента | вывод `openssl rand -hex 32` |
| `CONTROL_PLANE_API_KEY` | Внутренний API-ключ для связки бот-CP | вывод `openssl rand -hex 24` |
| `TELEGRAM_BOT_TOKEN` | Токен бота от @BotFather | `123456:ABC...` |

Никогда не копируйте dev-дефолты из `docker/docker-compose.yml` в прод. Генерируйте свежие секреты под каждое окружение.

## Эндпоинты и порты

Control plane (`:8110` / `:9090`):

| Эндпоинт | Описание |
| :--- | :--- |
| `/api/v1` | REST API (JWT или API-ключ) |
| `/admin` | Админ-консоль (cookie-сессия + CSRF) |
| `/sub/{token}` | Конфиги подписки пользователя (публичный токен-URL) |
| `/metrics` | Метрики Prometheus (требуется аутентификация) |
| `/healthz`, `/readyz` | Liveness и readiness пробы |

Агент ноды:

| Порт | Описание |
| :--- | :--- |
| `:8081/healthz`, `:8081/readyz` | Пробы агента |
| `:8081/metrics` | Метрики агента (`Bearer $VPNBUILDER_METRICS_TOKEN`) |
| `51820/udp` | WireGuard |
| `443/tcp` | VLESS-Reality |

Локальная разработка дополнительно: PostgreSQL `5432`, Redis `6379`, Vite dev-сервер `5173`.

## Структура проекта

```
.
├── .github/              # CI, релизы, CodeQL, скан секретов, шаблоны issues
├── api/                  # Спецификация OpenAPI 3.1
├── cmd/
│   ├── control-plane/    # Точка входа control plane
│   ├── agent/            # Точка входа агента (daemon и doctor)
│   └── bot/              # Точка входа Telegram-бота
├── deploy/
│   ├── cloud-init/       # Шаблоны облачных выходных нод
│   └── helm/             # Helm-чарт Kubernetes
├── docker/               # Dockerfile и Compose-стенд
├── docs/                 # Гайды и справочники
├── internal/
│   ├── controlplane/     # REST API, auth, сервисы, sqlc store, веб-консоль, AI-инструменты
│   ├── agent/            # WireGuard/AWG netlink, менеджер Xray, syncer, doctor
│   ├── bot/              # Движок Telegram-бота, платежи, i18n
│   └── shared/           # Конфиг, логгер, метрики, трейсинг
├── migrations/           # Миграции PostgreSQL (golang-migrate, up и down)
├── packaging/
│   └── systemd/          # systemd-юниты с sandboxing
├── pkg/
│   ├── openapi/          # Сгенерированные сервер и модели REST API
│   └── proto/            # Сгенерированные gRPC-стабы
├── proto/                # Protobuf-схема
├── scripts/              # Установщик, бэкап/восстановление БД, харденинг сервера
└── tests/                # Интеграционные и packaging-тесты
```

## Доступные команды

| Команда | Описание |
| :--- | :--- |
| `make build` | Собрать control-plane, agent и bot в `bin/` |
| `make test` | Все тесты с race detector |
| `make test-coverage` | Тесты с HTML-отчетом покрытия |
| `make test-integration` | Интеграционные тесты (тег `integration`) |
| `make lint` | Запустить golangci-lint |
| `make verify` | Tidy, lint и test |
| `make generate` | Перегенерировать Protobuf, sqlc и OpenAPI-код |
| `make migrate-up` / `make migrate-down` | Применить / откатить миграцию (нужен `DATABASE_URL`) |
| `make dev-up` / `make dev-down` / `make dev-logs` | Локальный стенд PostgreSQL + Redis |
| `make run-cp` / `make run-agent` | Live reload через air |
| `make docker-build` | Собрать образы control-plane и agent |
| `make release-check` / `make release-snapshot` | Проверка / сухой прогон GoReleaser-релиза |
| `make doctor` | Диагностика ноды |
| `make install-tools` | Установить buf, sqlc, oapi-codegen, migrate, golangci-lint, air |
| `make clean` | Удалить артефакты сборки |

## Тестирование

```bash
make test             # юнит-тесты, race detector
make test-coverage    # HTML-отчет покрытия
make test-integration # нужны Docker (testcontainers) и внешние демоны
```

Соглашения: табличные тесты рядом с кодом (`*_test.go`), интеграционные — в `tests/e2e`, packaging-проверки — в `tests/packaging`. Каждый security-фикс сопровождается регрессионным тестом.

## Деплой

- **Один сервер / выходная нода**: `scripts/install.sh` (`--all`, `--cp`, `--agent`), systemd-юниты из `packaging/systemd`.
- **Docker**: `docker/control-plane.Dockerfile`, `docker/agent.Dockerfile`, `docker/bot.Dockerfile`, Compose-стенд в `docker/`.
- **Kubernetes**: Helm-чарт в `deploy/helm` (JWT-секрет, DSN базы и адрес Redis — через Secrets).
- **Облачные ноды**: шаблоны в `deploy/cloud-init`.
- **Харденинг**: `scripts/harden-server.sh`; прод-гайд в `docs/DEPLOYMENT_GUIDE.md`.

Релизы режутся пушем тега `v*.*.*`: GoReleaser собирает мультиарх бинари, пакеты `.deb`/`.rpm`, SBOM и подписи Cosign; Docker-образы публикуются в GHCR.

## Документация

- [Архитектура системы](docs/ARCHITECTURE.md)
- [Прод-деплой](docs/DEPLOYMENT_GUIDE.md)
- [Гайд миграции](docs/MIGRATION_GUIDE.md)
- [Гайд разработчика](docs/DEVELOPMENT.md)
- [REST и gRPC API](docs/API_REFERENCE.md)
- [Гайд ручного тестирования](docs/MANUAL_TESTING_GUIDE.md)

## Автор и контакты

Разработка и поддержка — [ivanchik-byte](https://github.com/ivanchik-byte).

[![Telegram Chat](https://img.shields.io/badge/Telegram-Личка-26A5E4?logo=telegram)](https://t.me/ivanchikbyte)
[![Telegram Channel RU](https://img.shields.io/badge/Telegram-Канал_RU-26A5E4?logo=telegram)](https://t.me/ivanchik_byte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?logo=gmail)](mailto:ivanchikbyte@gmail.com)
[![Issues](https://img.shields.io/badge/GitHub-Issues-181717?logo=github)](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

Баги и фичи — в [GitHub Issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues). Уязвимости безопасности — только приватно, по [Security Policy](SECURITY.md).

## Участие в разработке

- [Гайд контрибьютора](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)
- [Кодекс поведения](CODE_OF_CONDUCT.md)

## Лицензия

Simple-VPN-Builder — open-source под [MIT License](LICENSE).
