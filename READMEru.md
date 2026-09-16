<div align="center">

[English](README.md) | **Русский**

# Simple VPN Builder

Промышленная платформа управления VPN-инфраструктурой и автоматизированный движок коммерческих продаж.  
Управляет WireGuard, AmneziaWG (защита от блокировок по DPI) и VLESS+Reality на распределенных серверах Linux из единого центра управления.

[![CI](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-1.0.0--dev-blue?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/dl/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-@ivanchikbyte-2CA5E0?style=flat-square&logo=telegram&logoColor=white)](https://t.me/ivanchikbyte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?style=flat-square&logo=gmail&logoColor=white)](mailto:ivanchikbyte@gmail.com)

</div>

---

## Быстрый старт

**Автоматический установщик одной командой (Debian / Ubuntu):**

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

**Docker Compose:**

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git && cd Simple-VPN-Builder
cp .env.example .env
make dev-up
```

После запуска откройте `http://IP_СЕРВЕРА:8110` в браузере.

> Инструкция для автономных ИИ-агентов и скриптов автоматизации: [installAI.md](installAI.md).

---

## Оглавление

- [Быстрый старт](#быстрый-старт)
- [Обзор](#обзор)
- [Архитектура](#архитектура)
- [Сравнение протоколов](#сравнение-протоколов)
- [Совместимость клиентов](#совместимость-клиентов)
- [Подключение нового узла](#подключение-нового-узла)
- [Конфигурация](#конфигурация)
- [Порты и протоколы](#порты-и-протоколы)
- [Контакты и поддержка](#контакты-и-поддержка)
- [Документация](#документация)
- [Лицензия](#лицензия)

---

## Обзор

Simple VPN Builder объединяет управление распределенной инфраструктурой и продажу подписок в единой системе:

- **Плоскость управления (`controlplane`)**: Центральный REST API, серверная веб-панель (HTMX + Alpine.js), маршрутизатор подписок, Telegram-бот продаж и ИИ-копилот серверов.
- **Агент узла (`agent`)**: Компактный демон для каждого VPN-сервера, управляющий ядром WireGuard, обфускацией AmneziaWG и Xray VLESS-Reality через netlink и системные процессы.

Связь между панелью и узлами осуществляется через постоянные gRPC-потоки с взаимной аутентификацией mTLS.

---

## Архитектура

```mermaid
sequenceDiagram
    autonumber
    actor User as Пользователь
    actor Admin as Администратор
    participant Bot as Telegram-бот
    participant CP as Панель управления :8110
    participant Hub as gRPC-хаб :9090
    participant Node as Агент узла

    Admin->>CP: Подключение нового сервера
    CP->>Hub: Регистрация и выпуск mTLS сертификата
    Hub->>Node: Установка защищенного соединения

    User->>Bot: Выбор тарифа и оплата CryptoBot / Stars
    Bot->>CP: Создание учетной записи и пира
    CP->>Hub: Передача публичного ключа на узел
    Hub->>Node: Настройка WireGuard / Xray через netlink
    Node-->>Hub: Пир успешно активирован
    CP-->>Bot: Ссылка на подписку /sub/token
    Bot-->>User: Выдача конфигурации и QR-кода

    Node->>Hub: Потоковая передача счетчиков трафика
    Hub->>CP: Учет трафика и контроль лимитов
```

---

## Сравнение протоколов

| Параметр | WireGuard | AmneziaWG | VLESS + Reality |
|---|---|---|---|
| Транспорт | UDP 51820 | UDP 51820 (измененные заголовки) | TCP 443 (маскировка под TLS) |
| Реализация в ОС | Модуль ядра через netlink | Модуль ядра / userspace прослойка | Процесс ядра Xray-core |
| Устойчивость к DPI | Отсутствует (стандартная сигнатура) | Высокая (мусорные пакеты, случайный размер) | Максимальная (маскировка под TLS 1.3) |
| Пропускная способность | Максимальная канальная | Близкая к максимальной | Высокая |
| Назначение | Доверенные каналы, серверные линки | Провайдеры с блокировкой WireGuard | Жесткая цензура, ТСПУ, корпоративные фильтры |

---

## Совместимость клиентов

| Клиент | Платформы | WireGuard | AmneziaWG | VLESS + Reality | Формат импорта |
|---|---|---|---|---|---|
| Sing-box | iOS, Android, macOS, Windows, Linux | Да | Да (с v1.9+) | Да | Ссылка в 1 клик / JSON |
| Streisand | iOS | Да | Нет | Да | Ссылка в 1 клик / Base64 |
| Shadowrocket | iOS | Да | Нет | Да | Ссылка в 1 клик / Base64 |
| Happ | iOS, Android | Да | Нет | Да | Ссылка в 1 клик / VLESS |
| v2rayNG | Android | Нет | Нет | Да | Ссылка в 1 клик / Base64 |
| AmneziaVPN | iOS, Android, macOS, Windows, Linux | Да | Да | Нет | Конфиг Amnezia JSON |
| WireGuard Official | Все платформы | Да | Нет | Нет | Файл `.conf` / QR-код |
| Clash Verge / Mihomo | macOS, Windows, Linux | Да | Нет | Да | Clash YAML |

---

## Подключение нового узла

Добавление сервера выполняется одной командой из панели администратора:

```bash
curl -fsSL https://IP_ВАШЕЙ_ПАНЕЛИ:8110/bootstrap/node.sh | bash -s --   --token "одноразовый-токен-регистрации"   --panel "https://IP_ВАШЕЙ_ПАНЕЛИ:8110"   --grpc "IP_ВАШЕЙ_ПАНЕЛИ:9090"
```

Скрипт устанавливает `vpn-agent`, генерирует локальные ключи, получает подписанный сертификат от внутреннего CA панели, конфигурирует `nftables` и подключается к gRPC-хабу за несколько секунд.

---

## Конфигурация

Настраиваются через файл `.env` или переменные окружения:

| Переменная | Обязательна | Описание |
|---|---|---|
| `DATABASE_URL` | Да | DSN подключения к PostgreSQL (`postgres://user:pass@host:5432/db`) |
| `REDIS_URL` | Да | DSN подключения к Redis (`redis://localhost:6379`) |
| `JWT_SECRET` | Да | Случайная строка от 64 символов для подписи токенов |
| `ADMIN_PASSWORD` | Да | Начальный пароль учетной записи администратора |
| `TELEGRAM_BOT_TOKEN` | Нет | Токен бота от @BotFather для продажи подписок |
| `CRYPTOBOT_TOKEN` | Нет | API-токен для приема платежей в криптовалюте |
| `AI_ENDPOINT` | Нет | Базовый URL OpenAI-совместимого сервиса |
| `AI_API_KEY` | Нет | API-ключ для встроенного ИИ-копилота |

Полный справочник и примеры настроек смотрите в [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Порты и протоколы

| Сервис | Порт | Протокол | Назначение |
|---|---|---|---|
| Панель и REST API | `8110` | TCP (HTTP/HTTPS) | Панель управления, выдача подписки `/sub/{token}`, REST API |
| gRPC-хаб | `9090` | TCP (mTLS) | Потоковая связь между панелью и узлами |
| Метрики агента | `8081` | TCP (HTTP) | Проверка состояния и сбор метрик Prometheus |
| WireGuard | `51820` | UDP | VPN-трафик клиентов WireGuard и AmneziaWG |
| Прокси VLESS Reality | `443` | TCP | Входящий трафик маскировки под TLS для Xray |

---

## Контакты и поддержка

По вопросам внедрения, поддержки или сотрудничества:

- **Разработчик**: Ivan Chik
- **Telegram**: [https://t.me/ivanchikbyte](https://t.me/ivanchikbyte) (`@ivanchikbyte`)
- **Email**: [ivanchikbyte@gmail.com](mailto:ivanchikbyte@gmail.com)
- **Сообщить об ошибке**: [https://github.com/ivanchik-byte/Simple-VPN-Builder/issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

---

## Документация

| Документ | Описание |
|---|---|
| [Инструкция для ИИ-агентов](installAI.md) | Неинтерактивные команды автоматической установки |
| [Архитектура](docs/ARCHITECTURE.md) | Дизайн системы, изоляция данных, диаграммы потоков |
| [Справочник конфигурации](docs/CONFIGURATION.md) | Все переменные окружения и файл `.env` |
| [Устранение неполадок](docs/TROUBLESHOOTING.md) | Решение типичных сетевых сбоев, sysctl, nftables, SSE |
| [Справочник REST API](docs/API_REFERENCE.md) | Все маршруты, схемы запросов/ответов и коды ошибок |
| [Руководство по развертыванию](docs/DEPLOYMENT_GUIDE.md) | Развертывание на bare metal, Docker, службы systemd |
| [Руководство разработчика](docs/DEVELOPMENT.md) | Локальное окружение, миграции и кодогенерация |
| [Руководство по миграции](docs/MIGRATION_GUIDE.md) | Перенос пользователей и подписок из 3X-UI и Marzban |
| [Руководство по тестированию](docs/MANUAL_TESTING_GUIDE.md) | Сценарии сквозного тестирования функционала |

---

## Лицензия

[MIT](LICENSE) - Ivan Chik
