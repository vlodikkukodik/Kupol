# КУПОЛ — деплой

Две независимые части:

| Часть | Куда | Чем |
|---|---|---|
| Фронтенд + PHP-прокси | shared-хостинг, `kupol.vladinc.ru`, FTP, каталог `kupol.vladinc.ru/www` | `deploy/deploy-front.sh` |
| Go API + PostgreSQL | VPS, `api.kupol.vladinc.ru`, nginx → Go на `127.0.0.1:8081` (8080 на этом VPS занят Apache) | `deploy/deploy-back.sh` |

Доступы лежат в `docs/secrets.local.md` (в git не попадает). Настройки скриптов — `deploy/.env.deploy`
(образец: `deploy/env.deploy.example`, тоже не коммитится).

Порядок всегда: **сначала бэкенд, потом фронтенд**, затем `deploy/check-site.sh` (`deploy-front.sh` запускает его сам).

## 1. Подготовка VPS (один раз)

Нужны PostgreSQL 15+ (рекомендуется 17: локаль ICU), nginx, certbot, curl. DNS: `api.kupol.vladinc.ru` → IP VPS.

```bash
# пользователи и каталоги
sudo useradd --system --home /opt/kupol --shell /usr/sbin/nologin kupol   # под ним работает API
sudo adduser --disabled-password deploy                                    # под ним заходит deploy-back.sh (SSH-ключ!)
sudo install -d -o deploy -g kupol -m 750 /opt/kupol
sudo install -d -o root -g root -m 755 /etc/kupol
sudo usermod -aG systemd-journal deploy                                    # чтобы скрипт показывал журнал при сбое
```

База данных (ICU-сортировка, чтобы кириллица сортировалась по-русски, как в dev):

```sql
CREATE ROLE kupol LOGIN PASSWORD '<openssl rand -hex 24>';
CREATE DATABASE kupol OWNER kupol ENCODING 'UTF8'
  LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0;
```

Настройки API — `/etc/kupol/kupol.env` по образцу `deploy/server/kupol.env.example`
(`chown root:root`, `chmod 600`). Секрет прокси: `openssl rand -hex 32` — **тот же** пойдёт в `api/config.php` на хостинге.
С этапа 1 в prod обязателен `KUPOL_SITE_ORIGIN=https://kupol.vladinc.ru`: без него API не запустится (при деплое новой версии
без этой строки `deploy-back.sh` откатится на прежнюю).

Админ-команды (назначить Директора, уровень, сбросить пароль) — через обёртку:

```bash
sudo install -m 755 deploy/server/kupol-admin.sh /usr/local/bin/kupol-admin
sudo kupol-admin user set-directorate <ваш_логин> on
```

Сервис и права на перезапуск:

```bash
sudo cp deploy/server/kupol.service /etc/systemd/system/kupol.service
sudo systemctl daemon-reload && sudo systemctl enable kupol
echo 'deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart kupol' | sudo tee /etc/sudoers.d/kupol-deploy
sudo chmod 440 /etc/sudoers.d/kupol-deploy && sudo visudo -c
```

nginx и TLS:

```bash
sudo cp deploy/server/nginx-kupol-api.conf /etc/nginx/conf.d/kupol-api.conf
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d api.kupol.vladinc.ru      # добавит 443 и редирект
```

Логи — journald (`journalctl -u kupol -f`); объём ограничьте в `/etc/systemd/journald.conf` (`SystemMaxUse=`).

## 2. Первый деплой бэкенда

```bash
cp deploy/env.deploy.example deploy/.env.deploy   # заполнить VPS_*, FTP_*
deploy/deploy-back.sh
curl https://api.kupol.vladinc.ru/health          # {"status":"ok","db":"ok",...}
```

Скрипт собирает бинарник (`make build-back`), кладёт его на VPS как `kupol.new`, сохраняет прежний как `kupol.prev`,
перезапускает сервис и ждёт `status: ok`. Если новая версия не стартовала — **сам откатывается** на `kupol.prev` и завершается с
ошибкой. Ручной откат: `deploy/deploy-back.sh --rollback`. Миграции БД применяет сам API при старте.

## 3. Первый деплой фронтенда

1. Создать боевой конфиг прокси из образца и залить его **один раз** (обычный деплой его не трогает и не удаляет):

   ```bash
   cp frontend/public/api/config.example.php /tmp/config.php     # вписать секрет = KUPOL_PROXY_SECRET с VPS
   deploy/deploy-front.sh --upload-config /tmp/config.php
   ```

2. Посмотреть, что лежит на хостинге и что будет удалено:

   ```bash
   deploy/deploy-front.sh --dry-run
   ```

3. В каталоге `kupol.vladinc.ru/www` сейчас лежит чужое содержимое — его нужно удалить. Скрипт удаляет посторонние файлы
   **только с явным флагом** и после показа списка:

   ```bash
   deploy/deploy-front.sh --wipe
   ```

   Без `--wipe` при наличии посторонних файлов скрипт покажет список и остановится. После первого деплоя каталог «наш»
   (в нём есть `api/index.php`), и дальше флаг не нужен: `deploy/deploy-front.sh`.

Заливка идёт в безопасном порядке: сначала новые файлы, затем `index.html`, и только потом удаляются устаревшие —
сайт не ломается посреди деплоя. В конце запускается `deploy/check-site.sh`.

## 4. Проверка сайта

```bash
deploy/check-site.sh https://kupol.vladinc.ru
```

Проверяет «снаружи»: history-режим (`.htaccess`/mod_rewrite), CSP и заголовки безопасности, кэш, индексацию (открыта только
главная и `/about`), закрытость `.htaccess` и служебных файлов PHP, цепочку PHP-прокси → Go → БД (принял ли Go подпись),
форматы ошибок. Печатает `client_ip`, который видит Go: должен быть ваш IP. Если там адрес хостинга (у хостинга свой обратный
прокси), задайте в `api/config.php` `ip_header` (например `HTTP_X_FORWARDED_FOR`) и залейте конфиг заново.

Подсказки по кодам ошибок `/api/health`: `proxy_misconfigured` — не залит или неверен `config.php`;
`upstream_unavailable`/`upstream_timeout` — прокси не достучался до Go (адрес, DNS, TLS, запущен ли `kupol`);
`bad_proxy_signature` — секрет в `config.php` не совпадает с `KUPOL_PROXY_SECRET` или разошлись часы (допуск ±60 с).

## 5. Резервные копии (ставятся до того, как авторы начнут писать)

Каждую ночь `deploy/backup/kupol-backup.sh` снимает дамп БД **в согласованном снимке** (`pg_dump --snapshot`: даже если сайт пишет
в БД во время копии, дамп и манифест — одной и той же БД), проверяет, что дамп читается, **шифрует `age`** публичным ключом
и хранит последние 14 копий на VPS; затем кладёт копию на shared-хостинг по FTPS — другая машина и другой провайдер
(последние 14). Если что-то не вышло, скрипт завершается с ошибкой (тревога мониторинга), а открытого дампа не остаётся никогда.

**Ключи.** На машине автора: `age-keygen -o kupol-backup.key` — приватный ключ в менеджер паролей и на второй носитель
(потеряете — копии не расшифровать; отдавать VPS его нельзя); `age-keygen -y kupol-backup.key` печатает публичный ключ.

**Установка на VPS** (один раз, под root; выполняется только по вашей команде):
```bash
apt install age lftp                       # pg_dump уже есть вместе с PostgreSQL
install -m 755 deploy/backup/kupol-backup.sh /usr/local/bin/kupol-backup
install -m 600 -o kupol -g kupol deploy/backup/backup.env.example /etc/kupol/backup.env   # заполнить: БД, публичный ключ, FTP
install -d -m 700 -o kupol -g kupol /var/backups/kupol
install -m 644 deploy/backup/kupol-backup.{service,timer} /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now kupol-backup.timer
systemctl start kupol-backup.service && journalctl -u kupol-backup -n 30   # первая копия сразу
```
FTP-каталог для копий — **вне** веб-каталога сайта (`kupol.vladinc.ru/backups`, рядом с `www`); лучше завести на хостинге
отдельного FTP-пользователя только для копий: пароль хранится на VPS в `/etc/kupol/backup.env` (права 600).

**Проверка восстановления — раз в неделю и после любых изменений схемы**, на машине с приватным ключом:
```bash
deploy/backup/kupol-restore-check.sh kupol-ГГГГММДД-ЧЧММСС.dump.age kupol-backup.key postgres://админ@localhost/postgres
```
Скрипт расшифровывает копию, восстанавливает её во **временную** БД, сверяет число строк каждой таблицы с манифестом и версию
схемы, после чего удаляет временную БД и открытый дамп. Копия, которую ни разу не восстанавливали, — не копия.

**Мониторинг:** в `backup.env` можно задать `HEALTHCHECK_URL` ([Healthchecks.io](https://healthchecks.io) и подобные): пинг
отправляется только после успешной копии, а если пинга нет больше суток, приходит тревога.

**Восстановление после потери БД:** `age -d -i kupol-backup.key -o kupol.dump kupol-….dump.age`, затем
`pg_restore --no-owner -d kupol kupol.dump` в пустую БД с ICU (см. §1). Файлы загрузок (этап 6) будут копироваться так же.

## Что проверено автоматически, а что нет

Проверено на настоящих компонентах (`make test-infra`): резервные копии (`make test-backup`: шифрование, согласованность
под параллельной записью, восстановление и сверка с манифестом, ротация на VPS и «хостинге», обнаружение порчи файла, чужого
ключа и подменённого манифеста, сбои БД/ключа/хостинга без полкопии, запрет одновременного запуска),
боевая сборка на Apache + PHP 8.3 (`.htaccess`, CSP, rewrite,
прокси, браузерные тесты), скрипты деплоя на настоящих sshd и FTP (доставка, откат сломанной версии, `--wipe`, сохранность
`config.php`), конфиг nginx (сырой URI и подпись, подделка `X-Forwarded-For`, лимит тела). боевая сборка на Apache + PHP 8.3 (`.htaccess`, CSP, rewrite,
прокси, браузерные тесты), скрипты деплоя на настоящих sshd и FTP (доставка, откат сломанной версии, `--wipe`, сохранность
`config.php`), конфиг nginx (сырой URI и подпись, подделка `X-Forwarded-For`, лимит тела).

**Не проверялось** (локально нечем): реальный хостинг и реальный VPS; FTP поверх TLS (`FTP_TLS=explicit`) — локально гонялся
FTP без шифрования; запуск `kupol.service` под systemd (в контейнере systemd нет; синтаксис юнита проверен
`systemd-analyze verify`) — после установки проверьте `systemctl status kupol` и `journalctl -u kupol`; WebSocket через nginx — появится вместе с чатом (этап 6), в конфиге заложены
заголовки `Upgrade`/`Connection`.
