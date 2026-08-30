# wf-api

Go backend for **Vaulzo** — a personal net-worth tracker. It stores assets and
liabilities in MongoDB, prices market-linked holdings from live feeds, and
records a daily net-worth snapshot per user that powers the history chart.

Frontend lives in the sibling [`wf-app`](https://github.com/sameerchoubey/wf-app)
repository (React, deployed separately).

## Stack

- Go 1.22, [chi](https://github.com/go-chi/chi) router
- MongoDB (assets, liabilities, users, snapshots, price caches)
- JWT auth (HS256, 7-day expiry)
- Fly.io deployment, GitHub Actions for deploys and the nightly job

## How valuation works

Manually-valued assets (cash, electronics, property…) store a plain INR
`current_value`. Market-linked assets store **quantities**, and the server is
the only pricing authority — clients never send a value for them:

| Asset type    | Holds                          | Priced from                                   |
| ------------- | ------------------------------ | --------------------------------------------- |
| `crypto`      | coins × units                  | FreeCryptoAPI (needs `CRYPTO_PRICE_API_KEY`)   |
| `mutual_fund` | schemes × units                | AMFI NAVs via [mfapi.in](https://mfapi.in)     |
| `us_stocks`   | tickers × units + USD cash     | Yahoo Finance (USD-quoted listings only)       |
| `gold`        | grams × karat (24/22/18)       | IBJA domestic rates (scraped, see below)       |
| travel points | programs × points × $/pt       | stored USD total × USD/INR                     |
| foreign cash  | amount × currency              | exchangerate-api USD/EUR→INR                   |

Every feed result is cached in its own Mongo collection (`crypto_prices`,
`mf_navs`, `stock_prices`, `gold_rates`, `exchange_rates`). The design is
**degrade gracefully, complain loudly**: a dead feed never overwrites or
zeroes anything — portfolios hold their last full valuation — but the failure
is logged with its cause and turns the nightly run red.

### Refresh triggers

The Fly machine auto-stops when idle, so in-process crons alone can't be
relied on. Three mechanisms keep prices current:

1. **Nightly job** — a GitHub Actions cron (`.github/workflows/daily-jobs.yml`,
   00:05 IST) POSTs to `/api/internal/daily` with the `JOB_TOKEN` secret. The
   request itself wakes the machine and the work runs inside it: FX → crypto →
   MF → stocks → gold → FX revaluation of foreign/travel assets → snapshot for
   every user. Steps that fail are collected and reported (HTTP 500 ⇒ red run)
   but never abort the chain — snapshots are written every night regardless.
2. **Refresh-on-read** — `GET /api/dashboard` refreshes any feed whose last
   attempt is older than its window (crypto/stocks 1 h; MF, gold, FX 12 h),
   synchronously and mutex-serialized, so the response already shows fresh
   values and concurrent requests cause a single upstream call.
3. **In-process crons** — backstop for whenever the machine happens to be
   awake (hourly crypto, daily snapshot/MF at UTC times).

### Snapshots

`CreateDailySnapshot` upserts one row per user per day with totals and a copy
of the holdings. Storage is UTC, but the day *bucket* rolls over at
**06:00 IST** (00:30 UTC), so the post-midnight nightly job stamps the day it
closes out and pre-dawn edits don't split days.

### The IBJA gold scrape

IBJA has no API; the rates page is fetched with a browser User-Agent and
parsed by regex for its stable label spans (`lblGold916_PM` …, ₹/10 g, PM
preferred). Overnight and on non-business days that table isn't rendered, so
the parser falls back to the always-present per-gram "compare" widget. Parsed
rates must exist for all three purities and satisfy 24K > 22K > 18K or the
fetch is rejected — a page redesign can never cache garbage.

## API

Public (per-IP rate-limited where noted):

```
POST /api/register           create account (rate-limited)
POST /api/login              → JWT (rate-limited)
POST /api/auth/lookup        does this email have an account? (rate-limited)
POST /api/internal/daily     nightly job hook (X-Job-Token; rate-limited)
GET  /api/exchange-rates     cached FX (INR per USD/EUR)
GET  /api/crypto-prices      cached coin prices
GET  /healthz
```

Authenticated (`Authorization: Bearer <jwt>`):

```
GET/POST/PUT/DELETE /api/assets[/{id}]         asset CRUD (server prices portfolios)
GET/POST/PUT/DELETE /api/liabilities[/{id}]    liability CRUD
GET  /api/dashboard                            totals + assets + liabilities (triggers refresh-on-read)
GET  /api/snapshots?start_date=&end_date=      net-worth history
GET  /api/mf/search?q=      fund search        GET /api/mf/nav?code=       scheme NAV
GET  /api/stocks/search?q=  ticker search      GET /api/stocks/quote?symbol= quote
GET  /api/gold/rates                           IBJA ₹/gram by karat
GET/PUT /api/me                                profile        POST /api/me/password  change password
```

## Running locally

```sh
cp .env.example .env   # fill in values (see below)
go run ./cmd/server    # listens on :8080
```

Environment (`.env` is loaded via godotenv; on Fly these are secrets):

| Var                    | Required | Notes                                             |
| ---------------------- | -------- | ------------------------------------------------- |
| `MONGO_URL`, `DB_NAME` | yes      | MongoDB connection                                |
| `JWT_SECRET`           | prod     | falls back to an insecure dev default when unset  |
| `CORS_ORIGINS`         | prod     | comma-separated; dev default is `*`               |
| `CRYPTO_PRICE_API_KEY` | for crypto | FreeCryptoAPI token; refresh skipped when unset |
| `JOB_TOKEN`            | for nightly job | `/api/internal/daily` is disabled when unset |
| `HTTP_ADDR`            | no       | default `:8080`                                   |

The frontend dev server must run on `localhost:3000` (CORS). Sanity check the
build with `./scripts/verify-build.sh`.

## Deployment

Pushes to `main` deploy to Fly via `.github/workflows/fly-deploy.yml`
(`FLY_API_TOKEN` repo secret). Manual: `fly deploy` from the repo root.
The nightly workflow needs the `JOB_TOKEN` repo secret matching the Fly
secret of the same name. The app scales to zero when idle; requests
auto-start it (~1 s cold start).
