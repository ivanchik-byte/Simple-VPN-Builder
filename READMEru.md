<div align="center">

[English](README.md) | **Русский**

# Simple VPN Builder

> **Статус: активная бета.** Готовлю первый релиз `0.1.0v`. Код уже работает у меня на боевых серверах, но API и структура конфигов еще могут точечно меняться.
> Наткнулись на баг или есть идея: открывайте [issue](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues) или пишите мне напрямую в Telegram [@ivanchikbyte](https://t.me/ivanchikbyte).

Селф-хостед платформа для своих VPN-серверов и продажи подписок на Go.  
Я написал этот проект, чтобы в одном месте рулить WireGuard, AmneziaWG (с мусорными пакетами против ТСПУ) и Xray VLESS+Reality на любых Linux-серверах из единой веб-панели, не городя костыли из десятка разных скриптов.

[![CI](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.1.0v-blue?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/dl/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-@ivanchikbyte-2CA5E0?style=flat-square&logo=telegram&logoColor=white)](https://t.me/ivanchikbyte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?style=flat-square&logo=gmail&logoColor=white)](mailto:ivanchikbyte@gmail.com)

</div>

---

## Быстрый старт

**Установка одной командой на чистый сервер (Debian / Ubuntu):**

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

**Через Docker Compose (удобно пощупать локально):**

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git && cd Simple-VPN-Builder
cp .env.example .env
make dev-up
```

После старта открывайте в браузере `http://IP_СЕРВЕРА:8110` (или сразу дашборд: `http://IP_СЕРВЕРА:8110/admin/dashboard-v2`).

> Для автоматического деплоя через скрипты или автономных ИИ-агентов есть отдельная инструкция: [installAI.md](installAI.md).

---

## Зачем я это написал

Я написал Simple VPN Builder для себя. Мне надоело жонглировать bash-скриптами для ключей WireGuard, держать 3X-UI или Marzban ради пары протоколов и отдельно дописывать ботов для приема оплат. Хотелось иметь один цельный проект на Go, который закрывает задачу целиком:

- **Панель управления (`vpnbuilder-cp`)**: ядро на REST API, веб-морда на React 19 (вкомпилирована прямо в бинарник через `embed.FS`, никаких лишних рантаймов на сервере), умная раздача подписок `/sub/{token}` под разные клиенты и Telegram-бот с оплатой через Telegram Stars и CryptoBot.
- **Агент узла (`vpnbuilder-agent`)**: компактный бинарник для каждого VPN-сервера, который слушает команды панели по gRPC (mTLS), настраивает netlink для WireGuard, мусорные заголовки AmneziaWG и дергает Xray без костылей.

Панель и узлы общаются через постоянные стримы gRPC с взаимной mTLS-аутентификацией. Добавили пользователя или сменили лимит трафика: агент подхватывает конфиг за секунду на лету, без дропа соединений и перезагрузок интерфейсов.

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

## Поддерживаемые протоколы

| Протокол | Транспорт | Как крутится на сервере | Защита от блокировок (DPI) | Зачем нужен |
|---|---|---|---|---|
| WireGuard | UDP 51820 | Модуль ядра через netlink | Нет, стандартные сигнатуры WG | Доверенные каналы, чистый интернет без цензуры |
| AmneziaWG | UDP 51820 (кастомные заголовки) | Модуль ядра или userspace | Высокая (мусорные пакеты, случайные размеры) | Когда провайдер режет или глушит стандартный WireGuard |
| VLESS + Reality | TCP 443 (маскировка под TLS) | Демон Xray-core в userspace | Максимальная (полная имитация чужого TLS 1.3) | Жесткие файрволы, белые списки, ТСПУ |

---

## Проверенные клиенты

| Клиент | Платформы | WireGuard | AmneziaWG | VLESS + Reality | Формат импорта |
|---|---|---|---|---|---|
| Sing-box | iOS, Android, macOS, Windows, Linux | Да | Да (с v1.9+) | Да | Ссылка в 1 клик или JSON |
| Streisand | iOS | Да | Нет | Да | Ссылка в 1 клик или Base64 |
| Shadowrocket | iOS | Да | Нет | Да | Ссылка в 1 клик или Base64 |
| Happ | iOS, Android | Да | Нет | Да | Ссылка в 1 клик или VLESS |
| v2rayNG | Android | Нет | Нет | Да | Ссылка в 1 клик или Base64 |
| AmneziaVPN | iOS, Android, macOS, Windows, Linux | Да | Да | Нет | Конфиг Amnezia JSON |
| WireGuard Official | Все платформы | Да | Нет | Нет | Файл `.conf` или QR-код |
| Clash Verge / Mihomo | macOS, Windows, Linux | Да | Нет | Да | Clash YAML |

---

## Подключение нового узла

Чтобы привязать новый Linux-сервер к кластеру, сначала создайте узел в панели (дашборд Ноды -> Добавить, или `POST /api/v1/nodes`) с именем, равным hostname сервера (неизвестные узлы отклоняются). Затем запустите на сервере:

```bash
curl -fsSL https://IP_ВАШЕЙ_ПАНЕЛИ:8110/bootstrap/node.sh | bash -s --   --panel "https://IP_ВАШЕЙ_ПАНЕЛИ:8110"   --grpc "IP_ВАШЕЙ_ПАНЕЛИ:9090"
```

Скрипт сам поставит `vpnbuilder-agent`, запишет `/etc/vpnbuilder/agent.yaml` и сразу поднимет постоянную связь с панелью. Если панель требует mTLS, положите сертификаты из `VPNBUILDER_AGENT_CA_CERT` / `CERT_FILE` / `KEY_FILE` до старта сервиса. Авторегистрация по токену (`--token`) зарезервирована под будущий релиз.

---

## Конфигурация

Все параметры задаются через файл `.env` или системные переменные окружения:

| Переменная | Обязательна | Назначение |
|---|---|---|
| `VPNBUILDER_DATABASE_DSN` | Да | Строка подключения к PostgreSQL (`postgres://user:pass@host:5432/db?sslmode=disable`) |
| `VPNBUILDER_REDIS_ADDR` | Да | Хост и порт Redis (`localhost:6379`) |
| `VPNBUILDER_AUTH_JWT_SECRET` | Да | Случайная строка от 32 символов для подписи токенов сессий |
| `CONTROL_PLANE_API_KEY` | Нет | Общий API-ключ для фоновых служб и бота (`dev-key-change-in-production`) |
| `TELEGRAM_BOT_TOKEN` | Нет | Токен от @BotFather, если запускаете бота продаж |
| `CRYPTOBOT_TOKEN` | Нет | Токен от @CryptoBot для приема криптовалюты |
| `CONTROL_PLANE_URL` | Нет | Адрес панели, к которому обращается бот (`http://localhost:8110`) |

> **Безопасность (обязательно к прочтению):**  
> При первом старте база создает начального пользователя `admin@vpnbuilder.local` с паролем `Admin1234!`.  
> Перед тем как выкатывать панель наружу:
> 1. Зайдите в панель (`/admin/dashboard-v2`).
> 2. В разделе Settings -> Administrators (`/admin/settings-v2`) заведите свой личный аккаунт с ролью **Owner** и надежным паролем.
> 3. Перелогиньтесь под своим новым аккаунтом и **удалите** `admin@vpnbuilder.local`.
> В коде стоит защита: система физически не даст удалить последнего оставшегося владельца, так что случайно заблокировать себя не получится.

Полный список всех настроек описан в [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Сетевые порты

| Сервис | Порт | Протокол | Назначение |
|---|---|---|---|
| Панель и REST API | `8110` | TCP (HTTP/HTTPS) | Веб-интерфейс, ссылки на подписку `/sub/{token}`, REST API |
| gRPC-хаб | `9090` | TCP (mTLS) | Потоковая связь панели с агентами узлов |
| Проверка здоровья агента | `8081` | TCP (HTTP) | Эндпоинт `/healthz` и экспорт метрик Prometheus |
| WireGuard и AmneziaWG | `51820` | UDP | Пользовательский VPN-трафик клиентов |
| Прокси VLESS Reality | `443` | TCP | Трафик Xray с маскировкой под TLS |

---

## Автор и контакты

Я пилю этот проект в соло. Если у вас появились вопросы, вы нашли баг или хотите обсудить внедрение:

- **Автор**: Иван Чик
- **Telegram**: [@ivanchikbyte](https://t.me/ivanchikbyte) (отвечаю быстрее всего)
- **Почта**: [ivanchikbyte@gmail.com](mailto:ivanchikbyte@gmail.com)
- **Баги и идеи**: [GitHub issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

---

## Документация

| Руководство | О чем рассказывает |
|---|---|
| [Инструкция для ИИ-агентов](installAI.md) | Четкие шаги без лишнего текста для автоматической установки скриптами и агентами |
| [Архитектура](docs/ARCHITECTURE.md) | Подробный разбор подсистем, схемы связей и устройство базы |
| [Параметры конфигурации](docs/CONFIGURATION.md) | Полный справочник переменных окружения, значения по умолчанию и пример боевого конфига |
| [Решение проблем](docs/TROUBLESHOOTING.md) | Типичные ошибки подключения, тюнинг sysctl, nftables и починка SSE |
| [Справочник API](docs/API_REFERENCE.md) | Все эндпоинты REST API, схема авторизации, форматы запросов и ответов |
| [Развертывание в продакшене](docs/DEPLOYMENT_GUIDE.md) | Пошаговая настройка под systemd, Docker и reverse proxy |
| [Разработка](docs/DEVELOPMENT.md) | Как развернуть проект локально, запускать миграции и генерировать код |
| [Миграция](docs/MIGRATION_GUIDE.md) | Как перенести пользователей и ключи из 3X-UI или Marzban |
| [Ручное тестирование](docs/MANUAL_TESTING_GUIDE.md) | Полный чек-лист проверок для валидации развернутой системы |

---

## Лицензия

[MIT](LICENSE) (c) Ivan Chik
