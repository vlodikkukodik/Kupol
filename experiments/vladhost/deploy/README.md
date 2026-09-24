# Деплой Vladhost на VPS

VPS `37.153.70.33` (Ubuntu 24.04). Панель: https://app.vladinc.ru. Сайты: `https://{site}.{user}.vladinc.ru`.

## Обновление

```bash
SSHPASS='…' ./deploy/deploy.sh      # или без SSHPASS, если настроен ssh-ключ
```

Скрипт собирает бинарь и фронтенд, заливает их и запускает `install-remote.sh`: подмена бинаря и фронтенда одним
`rename`, перезапуск службы, проверка `nginx -t` (при провале свои конфиги откатываются, чужие не затрагиваются),
проверка `/api/healthz`. Миграции БД применяются при старте службы.

## Первичная установка (уже сделана)

1. `deploy/prepare.sh` на сервере: swap 4 ГБ, пользователь `vladhost`, каталоги, БД и роль `vladhost`, `/etc/vladhost/env`.
2. `deploy.sh` (nginx подключит только http-часть, пока нет сертификата панели).
3. Сертификат панели:
   `certbot certonly --webroot -w /var/www/vladhost-acme -d app.vladinc.ru --cert-name app.vladinc.ru --deploy-hook "systemctl reload nginx"`
4. `deploy.sh` ещё раз — включится https-часть.
5. Администратор: `runuser -u vladhost -- bash -c 'set -a; . /etc/vladhost/env; VLADHOST_ADMIN_PASSWORD=… /opt/vladhost/bin/vladhost admin create --email … --username …'`

## Как устроены сертификаты сайтов

Панель работает без root и сама certbot запустить не может.

1. Создание сайта → панель кладёт пустой файл `/var/lib/vladhost/certs/queue/issue-{host}`.
2. `vladhost-certs.path` замечает файл → `vladhost-certs.service` запускает `/usr/local/lib/vladhost/certs.sh` (root).
3. Скрипт проверяет имя строгим шаблоном, выпускает сертификат (`certbot certonly --webroot`) и пишет результат
   в `/var/lib/vladhost/certs/status/{host}` (`ok` или `error: …`). Каталог `status` принадлежит root — панель
   не может подложить симлинк.
4. deploy-hook certbot (`cert-hook.sh`) копирует сертификат в `/etc/vladhost/certs/{host}`: воркеры nginx (www-data)
   не читают `/etc/letsencrypt/live`, где ключи других проектов. Тот же hook срабатывает при каждом продлении.
5. nginx берёт сертификат по SNI (`ssl_certificate /etc/vladhost/certs/$ssl_server_name/…`) — перезагрузка не нужна.
6. Панель раз в 5 секунд читает `status` и обновляет `sites.cert_status`. Заявка без ответа 30 минут → `failed`,
   пользователь может нажать «Повторить».

Удаление сайта → заявка `delete-{host}` → `certbot delete` и удаление копии.

## FTP

Встроенный FTP-сервер панели: `ftp.vladinc.ru:2121`, принимает FTPS (явный TLS, рекомендуется) и обычный FTP без шифрования (`VLADHOST_FTP_ALLOW_PLAIN=false` оставит только FTPS), пассивный режим, порты данных 50000–50100
(порт 21 занят стоковым proftpd, который мы не трогаем). Логин — `slug.username`, пароль отдельный на сайт.

- Сертификат `ftp.vladinc.ru` выпускается certbot'ом (webroot); deploy-hook `cert-hook.sh` кладёт копию в
  `/etc/vladhost/ftp/` (читает только группа vladhost). Сервер перечитывает файлы при смене, перезапуск не нужен.
- Если FTP не смог стартовать (нет сертификата, порт занят), панель работает дальше, в логе `ВНИМАНИЕ: FTP отключён`,
  в интерфейсе FTP показывается как недоступный.
- Переменные: `VLADHOST_FTP_ADDR`, `_HOST`, `_PUBLIC_IP`, `_PASSIVE_PORTS`, `_CERT`, `_KEY` (`deploy/prepare.sh` дописывает их в `/etc/vladhost/env`).
- Проверка с клиента: `lftp -u blog.имя,пароль -e "set ftp:ssl-force true" ftp.vladinc.ru:2121`.
- Открытые FTP-сессии не видят подмену каталога при деплое zip: запись в момент деплоя может уйти в старую версию.

## Полезное

```bash
journalctl -u vladhost -f              # панель
journalctl -u vladhost-certs -n 50     # выпуск сертификатов
ls /var/lib/vladhost/certs/queue       # непустая очередь = скрипт ещё работает или сломался
certbot certificates | grep -A3 vladinc
nginx -t && systemctl reload nginx
```

## Известные ограничения

- Лимит Let's Encrypt: 50 сертификатов в неделю на домен `vladinc.ru` (общий с другими проектами сервера).
  Сейчас прикрыт лимитом «1 сайт на аккаунт» и регистрацией по инвайтам.
- Сайты пользователей живут на соседних поддоменах основного домена. От подброса cookie защищает префикс
  `__Host-` у refresh-cookie, от запросов к панели с чужих страниц — проверка `Origin`. Полная изоляция
  потребовала бы отдельного домена для сайтов или записи в Public Suffix List.
- Стоковый proftpd на порту 21 принимает системных пользователей по паролю без TLS — это не Vladhost, но стоит
  решить, нужен ли он вообще (после отключения FTP панель можно перевести на порт 21).
- Вход на сервер по паролю root; лучше перейти на ключ и отключить парольный вход.
