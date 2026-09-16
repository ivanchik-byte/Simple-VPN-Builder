# Configuration Reference

All settings are configured via `.env` file, flags, or system environment variables.

## Environment Variables

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `DATABASE_URL` | String | Yes | - | PostgreSQL connection DSN (`postgres://user:pass@host:5432/dbname`) |
| `REDIS_URL` | String | Yes | `redis://localhost:6379` | Redis connection DSN for sessions and rate limiting |
| `HTTP_PORT` | Integer | No | `8110` | Port for the control plane REST API and Admin UI |
| `GRPC_PORT` | Integer | No | `9090` | Port for the mTLS gRPC hub |
| `JWT_SECRET` | String | Yes | - | Random 64-character secret for admin session tokens |
| `ADMIN_PASSWORD` | String | Yes | - | Initial master password for the `admin` account |
| `TOTP_ENABLED` | Boolean | No | `false` | Set to `true` to require TOTP second-factor on admin login |
| `TELEGRAM_BOT_TOKEN` | String | No | - | API token obtained from @BotFather for the sales bot |
| `CRYPTOBOT_TOKEN` | String | No | - | API token for CryptoBot cryptocurrency payment gateway |
| `AI_ENDPOINT` | String | No | - | OpenAI-compatible base URL (e.g. `https://api.openai.com/v1`) |
| `AI_API_KEY` | String | No | - | API key for the AI Infrastructure Copilot model |
| `AI_MODEL` | String | No | `gpt-4o` | Model identifier used by the AI Copilot |
| `SUB_BASE_URL` | String | No | - | Public URL base for client subscription delivery links |
| `LOG_LEVEL` | String | No | `info` | Logging verbosity: `debug`, `info`, `warn`, `error` |

## Production Example (.env)

```env
DATABASE_URL=postgres://vpnbuilder:YourSecurePassword@127.0.0.1:5432/vpnbuilder
REDIS_URL=redis://127.0.0.1:6379
HTTP_PORT=8110
GRPC_PORT=9090
JWT_SECRET=generate-random-64-character-secret-key-here
ADMIN_PASSWORD=SetAStrongInitialPasswordHere
TOTP_ENABLED=true
SUB_BASE_URL=https://sub.yourdomain.com
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrSTUvwxYZ
CRYPTOBOT_TOKEN=12345:AAABBBCCCDDDEEEFFF
AI_ENDPOINT=https://api.openai.com/v1
AI_API_KEY=sk-proj-your-api-key
AI_MODEL=gpt-4o
LOG_LEVEL=info
```
