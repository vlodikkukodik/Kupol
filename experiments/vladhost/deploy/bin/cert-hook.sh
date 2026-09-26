#!/bin/bash
# deploy-hook certbot (root): раскладывает копии выпущенного или продлённого сертификата тем, кто его читает.
# certbot передаёт RENEWED_LINEAGE=/etc/letsencrypt/live/{имя}. В /etc/letsencrypt/live лежат ключи и других проектов,
# поэтому службы Vladhost получают только копии своих сертификатов с узкими правами:
#   ftp.vladinc.ru  → /etc/vladhost/ftp/            (FTP-сервер панели, группа vladhost; перечитывает файлы сам)
#   db.vladinc.ru   → /etc/vladhost/db/pg/, maria/  (кластер PostgreSQL vhdb и MariaDB; перезагрузка TLS)
#   mail.vladinc.ru → /etc/vladhost/mail/tls/       (exim и dovecot; перезагрузка, если не задан VH_MAIL_NO_RELOAD)
#   app., webmail.  → ничего не копируется, nginx читает live/ сам; только перезагрузка nginx
#   остальные (адреса сайтов, их поддомены, свои домены) → /etc/vladhost/certs/{имя} для nginx (SNI) + сведения для панели.
set -uo pipefail

CERTS=${VH_CERTS_DIR:-/etc/vladhost/certs}
INFO=${VH_INFO:-/usr/local/lib/vladhost/cert-info.sh}
PGVER=${VH_PG_VERSION:-16}
LINEAGE=${RENEWED_LINEAGE:-}
NAME_RE='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'

[ -n "$LINEAGE" ] || { echo "cert-hook: не задан RENEWED_LINEAGE" >&2; exit 1; }
name=${LINEAGE%/}
name=${name##*/}
[[ ${#name} -le 253 && $name =~ $NAME_RE ]] || { echo "cert-hook: недопустимое имя $name" >&2; exit 1; }
[ -r "$LINEAGE/fullchain.pem" ] && [ -r "$LINEAGE/privkey.pem" ] || { echo "cert-hook: нет файлов в $LINEAGE" >&2; exit 1; }

# put каталог владелец группа режим_ключа — копия пары в каталог, атомарно по файлу.
put() {
    local dir=$1 owner=$2 group=$3 keymode=$4 f
    install -d -m 0750 -o root -g "$group" "$dir"
    for f in fullchain.pem privkey.pem; do
        local mode=0644
        [ "$f" = privkey.pem ] && mode=$keymode
        install -m "$mode" -o "$owner" -g "$group" "$LINEAGE/$f" "$dir/.$f.new"
        mv -f "$dir/.$f.new" "$dir/$f"
    done
}

active() { systemctl is-active --quiet "$1" 2>/dev/null; }

case $name in
    ftp.vladinc.ru)
        put /etc/vladhost/ftp root vladhost 0640
        ;;
    db.vladinc.ru)
        if id vhdb >/dev/null 2>&1; then
            put /etc/vladhost/db/pg vhdb vhdb 0600
            active "postgresql@$PGVER-vladhost" && systemctl reload "postgresql@$PGVER-vladhost"
        fi
        if id mysql >/dev/null 2>&1; then
            put /etc/vladhost/db/maria mysql mysql 0600
            # MariaDB 10.4+ перечитывает сертификат без перезапуска.
            active mariadb && { mariadb -e 'FLUSH SSL' 2>/dev/null || systemctl restart mariadb; }
        fi
        ;;
    mail.vladinc.ru)
        group=root
        getent group Debian-exim >/dev/null && group=Debian-exim # exim читает ключ под своим пользователем
        put /etc/vladhost/mail/tls root "$group" 0640
        # Службы перезагружаем, только когда работают на наших настройках (prepare-mail.sh вызывает хук раньше).
        if [ -z "${VH_MAIL_NO_RELOAD:-}" ] && [ -f /etc/dovecot/conf.d/99-vladhost.conf ]; then
            active dovecot && systemctl reload dovecot
            active exim4 && systemctl restart exim4
        fi
        ;;
    app.vladinc.ru | webmail.vladinc.ru)
        systemctl reload nginx
        ;;
    *)
        # Копия для nginx: новый каталог в .store и атомарная замена ссылки {имя} → воркеры не увидят половину пары.
        install -d -m 0750 -o root -g www-data "$CERTS" "$CERTS/.store"
        new=$(mktemp -d "$CERTS/.store/$name.XXXXXX") || exit 1
        chown root:www-data "$new"
        chmod 0750 "$new"
        install -m 0644 -o root -g www-data "$LINEAGE/fullchain.pem" "$new/fullchain.pem"
        install -m 0640 -o root -g www-data "$LINEAGE/privkey.pem" "$new/privkey.pem"
        old=""
        [ -L "$CERTS/$name" ] && old=$(readlink -f "$CERTS/$name")
        [ -d "$CERTS/$name" ] && [ ! -L "$CERTS/$name" ] && rm -rf -- "${CERTS:?}/$name" # старая раскладка без .store
        ln -sfn "$new" "$CERTS/.$name.link"
        mv -Tf "$CERTS/.$name.link" "$CERTS/$name"
        [[ -n $old && $old != "$new" && $old == "$(readlink -f "$CERTS")/.store/"* ]] && rm -rf -- "$old"
        "$INFO" "$name" >/dev/null 2>&1 || true
        ;;
esac
exit 0
