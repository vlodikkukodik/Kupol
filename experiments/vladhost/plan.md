# План: Vladhost

## 1. Суть

Бесплатный all-in-one для разработчиков: статический хостинг (потом динамика), веб-файловый менеджер, панель; позже свой DNS, BaaS, CLI/SDK.

| | |
|---|---|
| Код | `experiments/vladhost/` (этот monorepo) |
| Панель | `https://app.vladinc.ru` |
| Сайты юзеров | `https://{site}.{user}.vladinc.ru` (2 уровня) |
| Пример | `blog.john.vladinc.ru` |
| Стек | Go + Gin · PostgreSQL + goose + GORM · Vue 3 + TS + Vite + Naive UI + Pinia + Router + Zod |
| VPS | `37.153.70.33`, 4 ГБ RAM, Ubuntu 24.04 |
| HTTPS | 443 работает снаружи; **per-site LE HTTP-01** |
| DNS | `*.vladinc.ru` (и глубже) → VPS; apex/kupol → `78.108.80.36` |
| Доступ | регистрация **по инвайту**, только бесплатно |
| UI | русский, тёмная dev-тема |
| Ритм | соло, без сроков; MVP без DNS/BaaS |

## 2. Сервер (после удаления ISPmanager)

**Живое (не трогать):**

| Служба | Порт/роль |
|--------|-----------|
| nginx | 80, 443, 8443 |
| BIND (named) | 53 на `37.153.70.33` (зон пока нет) |
| proftpd | 21 |
| exim4 + dovecot | SMTP/почта (инвайты) |
| PostgreSQL | 127.0.0.1:5432 |
| Redis | 6379 |
| fail2ban, cloudflared, ssh | — |
| kupol | :8081 |
| registro | :9000 (fe), :8082 (api) |

**Удалено:** ISPmanager, coremanager, isp-php, roundcube, apache/PHP панели, `/usr/local/mgr5`.  
**Нет:** Docker, swap (**нужен swap 4 ГБ**).  
**Бэкап чистки:** `/root/backup-ispm-20260923-161729/`.  
**Опционально:** снести MySQL (только мусор roundcube/phpmyadmin).

**HTTPS:443** — снаружи работает (проверено). Edge/CF не обязателен. Фолбэк: `:8443` уже слушает nginx.

## 3. Хостинг-схема

```
app.vladinc.ru                 → панель (SPA + /api → Go)
{site}.{user}.vladinc.ru       → статика сайта
  blog.john.vladinc.ru         → /data/vladhost/sites/blog.john/public
```

### DNS

- Majordomo (NS `vladinc.ru`) резолвит **любую глубину** поддоменов на `37.153.70.33` (проверено: 1/2/3 уровня).
- `kupol.vladinc.ru`, apex `vladinc.ru` → `78.108.80.36` (другой сервер — не трогать).
- `app.vladinc.ru` → VPS (в wildcard, ок).
- Для юзерских зон (свой домен) — фаза 7: делегирование на `ns.vladinc.ru`/`ns2` (один IP — SPOF, ок на старте).

### TLS

- Wildcard LE `*.vladinc.ru` покрывает **только один уровень** — **не** подходит для `blog.john.vladinc.ru`.
- **Per-site certbot HTTP-01** на точное имя хоста.
- При создании/деплое сайта: nginx location `/.well-known/acme-challenge/` → `certbot --nginx -d {host}` → renew via certbot.timer.
- `app.vladinc.ru` — отдельный cert (HTTP-01).

### Nginx

- `server app.vladinc.ru` → SPA `frontend/dist` + `proxy_pass` `/api` → Go.
- Regex-default server: `server_name ~^(?<site>[^.]+)\.(?<user>[^.]+)\.vladinc\.ru$` → root `/data/vladhost/sites/$site.$user/public` (+ ACME location).
- Существующие vhosts kupol/registro/api.* — не трогать.


## 5. Модель данных (PostgreSQL, v1)

Миграции: `backend/internal/database/migrations/` (goose, вшиты в бинарь).

| Таблица | Поля | Заметки |
|---------|------|---------|
| `users` | id, email, username, password_hash (bcrypt), role (`admin`/`user`), created_at | уникальны email и username (в нижнем регистре); username — DNS-метка, есть список зарезервированных имён |
| `invites` | id, code, created_by, used_by, used_at, expires_at | одноразовые; использование в одной транзакции с созданием юзера (`FOR UPDATE`) |
| `refresh_tokens` | id, user_id, token_hash (sha256), expires_at, revoked_at | ротация при каждом обновлении; повтор старого токена закрывает все сессии юзера |
| `sites` | id, user_id, slug, host, disk_bytes, status (`empty`/`live`), deployed_at, created_at | host уникален; `(user_id, slug)` уникален; файлы в `{SITES_ROOT}/{host}/public` |


**Квоты:** 500 МБ диск, **1 сайт**/юзер (потом больше).  
**FS:** `/data/vladhost/sites/{host}/public`

**Host генерация:** `{site-slug}.{username}.vladinc.ru` — username из профиля, slug из формы.

## 6. API

Все пути под `/api`, ошибки: `{"error":{"code","message","field?"}}`.

| Метод | Путь | Доступ | Что делает |
|-------|------|--------|------------|
| POST | `/auth/register` | инвайт | создаёт юзера, сразу выдаёт сессию |
| POST | `/auth/login` | — | вход по email или имени |
| POST | `/auth/refresh` | cookie | ротация refresh, новый access |
| POST | `/auth/logout` | cookie | закрывает refresh |
| GET | `/me` | user | текущий пользователь |
| GET/POST | `/invites` | admin | список / создание (1–720 ч, по умолчанию 7 дней) |

| GET/POST | `/sites` | user | список с лимитами / создание `{slug}` |
| DELETE | `/sites/:id` | владелец | удаляет сайт и файлы |
| POST | `/sites/:id/deploy` | владелец | multipart `file` = zip; заменяет сайт целиком |

| GET | `/sites/:id/files?path=` | владелец | список папки (папки первыми) |
| GET/PUT | `/sites/:id/file?path=` | владелец | чтение / сохранение текстового файла (до 2 МБ, только UTF-8) |
| POST | `/sites/:id/files/mkdir`, `/files/rename` | владелец | новая папка / переименование и перенос |
| POST | `/sites/:id/files/upload?path=dir` | владелец | загрузка файла в папку (multipart `file`) |
| DELETE | `/sites/:id/files?path=` | владелец | удаление файла или папки (корень нельзя) |

Файлы: все операции идут через `os.Root` — ядро не даёт выйти за `public` ни через `..`, ни через симлинки. Запись атомарна (временный файл + rename), квота считается по факту, `disk_bytes` пересчитывается после каждой операции.

| POST | `/sites/:id/ftp` | владелец | включает FTP или меняет пароль; пароль в ответе один раз (bcrypt в БД) |
| DELETE | `/sites/:id/ftp` | владелец | отключает FTP, открытые сессии закрываются |

**FTP (фаза 5).** Вместо proftpd сделан встроенный сервер на `ftpserverlib` (`internal/ftpd`). Причины: на сервере стоит
стоковый proftpd без TLS-модуля и SQL (вход по системным аккаунтам, пароли открытым текстом; он не наш — не трогаем),
а квоту диска штатными средствами proftpd без `mod_quotatab` не обеспечить. Свой сервер: только FTPS (без TLS отказ),
без root, пути через `os.Root` (те же гарантии, что у файлового менеджера), квота резервируется на каждую запись и
общая для параллельных сессий, пароль на сайт (логин `slug.username`), лимит неудачных входов по IP, отзыв доступа
действует на открытые сессии. Порт 2121 (21 занят proftpd), пассивные порты 50000–50100.

Деплой: в корне архива нужен `index.html` (одна корневая папка снимается автоматически); защита от zip-slip, симлинков, дублей и zip-бомб (лимит считается по факту распаковки); подмена каталога через rename, при ошибке старая версия остаётся.

`/auth/*` ограничены по IP (20 в минуту, всплеск 10).

**Auth:** email+пароль, invite, rate limit.  
Access JWT (15 мин, живёт в памяти вкладки) + refresh (30 дней, httpOnly SameSite=Strict cookie, путь `/api/auth`).  
Первый админ: `VLADHOST_ADMIN_PASSWORD=… vladhost admin create --email E --username U`.  
**Email-подтверждение** отложено до деплоя: нужна настройка exim (SPF/DKIM), иначе письма уйдут в спам.  
Позже: GitHub OAuth, TOTP, passkeys.

Панель — UI-only (без публичного panel-API). BaaS-API — отдельная фаза.

## 7. Панель (Vue)

Разделы:
- **Dashboard** — диск, статус сайтов, активность
- **Сайты** — CRUD, host, статус cert, кнопка Deploy
- **Файлы** — браузер + CodeMirror 6
- **Настройки** — аккаунт, пароль, инвайты (admin)
- **Админ** — пользователи, квоты

Naive UI, тёмная тема, русский.

## 8. Фазы

| # | Объём | Done when |
|---|--------|-----------|
| **0** | Scaffold monorepo, Makefile, .env, lint/test | `make test` зелёный локально |
| **1** | Auth + оболочка панели | invite → login → layout |
| **2** | Sites + host + zip deploy + nginx + квоты | `blog.john.vladinc.ru` отдаёт index.html |
| **3** | File manager + CodeMirror | правка файлов в браузере |
| **4** | Per-site LE + systemd + deploy на VPS | HTTPS на хосте сайта, панель на app.vladinc.ru |
| **5** ✅ | FTP: свой FTPS-сервер в панели (`ftp.vladinc.ru:2121`), отдельный пароль на сайт | деплой по FTP |
| **6** | Динамика (Docker, Node/Python/PHP, лимиты) | первый dynamic app |
| **7** | DNS (зоны BIND / miekg, панель записей) | делегирование юзер-домена |
| **8** | BaaS (s3-lite, REST+WS, functions, cron) | модули в панели |
| **9** | DX: Go CLI, SDK (JS/TS, Go, Python), VitePress, status page | `vh deploy` |

**MVP = фазы 0–4.**

## 9. Инфраструктура VPS (при деплое)

1. **swapfile 4 ГБ** (критично при 4 ГБ RAM).
2. Не трогать: nginx vhosts kupol/registro/api.*, BIND, proftpd, exim, PG, Redis, cloudflared.
3. Host nginx: добавить `app.vladinc.ru` + regex server для `*.*.vladinc.ru`.
4. `certbot --nginx -d app.vladinc.ru`.
5. Директории `/data/vladhost/...`.
6. Systemd: `vladhost.service` (Go-бинарь).
7. MVP — **без Docker** (Go + system PG); Docker — к фазе динамики.
8. Опционально: убрать MySQL.

## 10. Тесты и качество

- Go: `go test`, golangci-lint
- FE: vue-tsc, eslint, Vitest
- Playwright: login → create site → deploy zip → открыть host
- `scripts/smoke.sh` на VPS

## 11. Риски

| Риск | Митигация |
|------|-----------|
| 4 ГБ + чужие сервисы | swap 4ГБ; MVP без Docker; квоты минимальные |
| Per-site LE rate limit (50/нед/домен) | 1 сайт/юзер; кэш certs; renew заранее |
| ns/ns2 один IP | принять для v1; позже 2-й IP |
| Бесплатно = абьюз | invite, rate limit, 500 МБ, 1 сайт |
| named vs свой DNS | фаза 7: сначала зоны в BIND, потом miekg если нужно |
| Path → subdomain | статика на поддоменах; dynamic позже (срез префикса или поддомены) |

## 12. Первые шаги реализации

1. Фаза 0: scaffold в `experiments/vladhost`
2. Фаза 1: auth + каркас панели
3. Фаза 2–4: сайты, деплой, файлы, TLS → рабочий MVP

---

*Статус: **MVP (фазы 0–4) готов и развёрнут на VPS.** Панель https://app.vladinc.ru, сайты на `{site}.{user}.vladinc.ru` с персональным сертификатом Let's Encrypt. На бою проверено curl'ом: регистрация по инвайту, создание сайта, выпуск сертификата за ~20 с, деплой zip, отдача по https, редирект с http, блок симлинков и дот-файлов, симуляция продления; соседние сервисы (kupol, registro) не затронуты. Эксплуатация: `deploy/README.md`. Фаза 5 (FTP) готова и развёрнута — см. §6. Дальше — фаза 6 (динамика).*

**Отклонения от исходного плана:** каталог сайта называется полным host (`/data/vladhost/sites/blog.john.vladinc.ru/public`); сертификаты выпускает отдельная root-служба по заявкам от панели (не `certbot --nginx`), копии для nginx лежат в `/etc/vladhost/certs`; email-подтверждение отложено (нужны SPF/DKIM в exim).
