#!/usr/bin/env bash
# Проверка развёрнутого сайта «снаружи», как это видит браузер: rewrite для history-режима,
# заголовки безопасности и кэша, PHP-прокси и цепочка до Go API, закрытость служебных файлов.
#
#   deploy/check-site.sh https://kupol.vladinc.ru
#
# Код выхода: 0 — критичных проблем нет, 1 — есть FAIL. WARN — необязательные возможности
# хостинга (сжатие), на работу сайта не влияют.
set -uo pipefail

BASE="${1:-}"
[ -n "$BASE" ] || { echo "использование: $0 <URL сайта>, например https://kupol.vladinc.ru"; exit 2; }
BASE="${BASE%/}"
for bin in curl jq; do command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 2; }; done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

FAILS=0
WARNS=0
ok()   { echo "  ok    $1"; }
fail() { echo "  FAIL  $1"; FAILS=$((FAILS + 1)); }
warn() { echo "  WARN  $1"; WARNS=$((WARNS + 1)); }
check() { # описание, фактическое, ожидаемое
  if [ "$2" = "$3" ]; then ok "$1"; else fail "$1: получено '$2', ожидалось '$3'"; fi
}

# fetch <имя> <curl-аргументы...>: тело -> $TMP/<имя>.body, заголовки -> $TMP/<имя>.head, код -> $TMP/<имя>.code
fetch() {
  local name="$1"; shift
  curl -sS --max-time 30 -D "$TMP/$name.head" -o "$TMP/$name.body" -w '%{http_code}' "$@" >"$TMP/$name.code" 2>"$TMP/$name.err" \
    || echo 000 >"$TMP/$name.code"
}
code() { cat "$TMP/$1.code"; }
# заголовок (без учёта регистра) из последнего блока ответа
hdr() { tr -d '\r' <"$TMP/$1.head" | awk -v k="$(echo "$2" | tr 'A-Z' 'a-z')" 'BEGIN{FS=": "} {if (tolower($1)==k) v=substr($0, length($1)+3)} END{print v}'; }

echo "Проверяю $BASE"

echo "== страницы и history-режим"
fetch root "$BASE/"
check "GET / -> 200" "$(code root)" 200
grep -q '<div id="app">' "$TMP/root.body" && ok "/ отдаёт index.html приложения" || fail "/ не похож на index.html приложения"
case "$(hdr root content-type)" in text/html*) ok "content-type text/html";; *) fail "content-type: '$(hdr root content-type)'";; esac

fetch spa "$BASE/doc/O-041"
check "GET /doc/O-041 -> 200 (rewrite на index.html)" "$(code spa)" 200
grep -q '<div id="app">' "$TMP/spa.body" && ok "SPA-маршрут отдаёт index.html" || fail "SPA-маршрут не отдаёт index.html — не работает mod_rewrite/.htaccess"

echo "== заголовки"
[ -n "$(hdr root content-security-policy)" ] && ok "Content-Security-Policy есть" || fail "нет Content-Security-Policy — не работает mod_headers/.htaccess"
check "X-Content-Type-Options" "$(hdr root x-content-type-options)" nosniff
check "X-Frame-Options" "$(hdr root x-frame-options)" DENY
check "index.html: Cache-Control" "$(hdr root cache-control)" no-cache
[ -z "$(hdr root x-robots-tag)" ] && ok "главная индексируется (нет X-Robots-Tag)" || fail "главная закрыта от индексации: '$(hdr root x-robots-tag)'"
case "$(hdr spa x-robots-tag)" in noindex*) ok "прочие страницы: noindex";; *) fail "прочие страницы должны быть noindex, получено '$(hdr spa x-robots-tag)'";; esac
fetch rootq "$BASE/?utm_source=test"
[ -z "$(hdr rootq x-robots-tag)" ] && ok "главная с query-строкой индексируется" || fail "/?utm_source=test закрыта от индексации: '$(hdr rootq x-robots-tag)'"
fetch about "$BASE/about"
[ -z "$(hdr about x-robots-tag)" ] && ok "/about индексируется" || fail "/about закрыта от индексации: '$(hdr about x-robots-tag)'"
fetch idx "$BASE/index.html"
case "$(hdr idx x-robots-tag)" in
  noindex*) ok "/index.html (дубликат главной): noindex" ;;
  *) warn "/index.html (дубликат главной) не помечен noindex — вероятно, статику отдаёт nginx хостинга в обход .htaccess; на работу сайта не влияет" ;;
esac

echo "== статика сборки"
ASSET="$(grep -o '/assets/[^"]*\.js' "$TMP/root.body" | head -1)"
if [ -n "$ASSET" ]; then
  fetch js "$BASE$ASSET"
  check "$ASSET -> 200" "$(code js)" 200
  case "$(hdr js cache-control)" in
    *immutable*) ok "хэшированный файл: Cache-Control immutable" ;;
    *) warn "у $ASSET нет Cache-Control immutable (сейчас '$(hdr js cache-control)') — статику отдаёт nginx хостинга в обход .htaccess; файлы хэшированы, поэтому функционально не страшно, но кэшируются хуже" ;;
  esac
  fetch jsgz -H 'Accept-Encoding: gzip' "$BASE$ASSET"
  [ "$(hdr jsgz content-encoding)" = gzip ] && ok "сжатие gzip включено" || warn "сжатие gzip не включено (mod_deflate) — сайт работает, но медленнее"
else
  fail "в index.html не нашёл ссылку на /assets/*.js"
fi
FONT="$(grep -o '/assets/[^"]*\.woff2' "$TMP/root.body" | head -1)"
if [ -z "$FONT" ] && [ -n "$ASSET" ]; then
  CSS="$(grep -o '/assets/[^"]*\.css' "$TMP/root.body" | head -1)"
  [ -n "$CSS" ] && { fetch css "$BASE$CSS"; FONT="$(grep -o '/assets/[^)"]*\.woff2' "$TMP/css.body" | head -1)"; }
fi
if [ -n "$FONT" ]; then
  fetch font "$BASE$FONT"
  check "шрифт $FONT -> 200" "$(code font)" 200
  check "шрифт: content-type" "$(hdr font content-type | cut -d';' -f1)" font/woff2
else
  fail "не нашёл woff2-шрифты в сборке"
fi

echo "== служебные файлы закрыты"
fetch htaccess "$BASE/.htaccess"
case "$(code htaccess)" in 403|404) ok ".htaccess недоступен ($(code htaccess))";; *) fail ".htaccess отдаётся наружу: $(code htaccess)";; esac
fetch listing "$BASE/assets/"
case "$(code listing)" in 403|404) ok "листинг каталога закрыт ($(code listing))";; 200) grep -qi 'index of' "$TMP/listing.body" && fail "открыт листинг каталога /assets/" || ok "/assets/ отдаёт SPA, не листинг";; *) ok "листинг каталога: $(code listing)";; esac
for f in config.php lib.php index.php config.example.php; do
  fetch "priv_$f" "$BASE/api/$f"
  body="$(cat "$TMP/priv_$f.body")"
  if echo "$body" | grep -q '<?php'; then
    fail "/api/$f отдаёт исходный код PHP!"
  elif echo "$body" | grep -qi 'secret'; then
    fail "/api/$f раскрывает содержимое"
  else
    ok "/api/$f не раскрывает файл (код $(code "priv_$f"))"
  fi
done

echo "== PHP-прокси и Go API"
fetch health "$BASE/api/health"
check "GET /api/health -> 200" "$(code health)" 200
if [ "$(code health)" = 200 ]; then
  check "status" "$(jq -r .status <"$TMP/health.body" 2>/dev/null)" ok
  check "db" "$(jq -r .db <"$TMP/health.body" 2>/dev/null)" ok
  check "ip_source (Go принял подпись PHP-прокси)" "$(jq -r .ip_source <"$TMP/health.body" 2>/dev/null)" proxy
  echo "        client_ip, который видит Go: $(jq -r .client_ip <"$TMP/health.body")  <- должен быть ВАШ IP; если это адрес хостинга, задайте ip_header в api/config.php"
  echo "        версия Go API: $(jq -r .version <"$TMP/health.body")"
  case "$(hdr health x-request-id)" in ????????????????) ok "X-Request-Id проходит";; *) fail "X-Request-Id: '$(hdr health x-request-id)'";; esac
  [ -z "$(hdr health x-powered-by)" ] && ok "X-Powered-By скрыт" || fail "утекает X-Powered-By: $(hdr health x-powered-by)"
  check "Cache-Control /api/health" "$(hdr health cache-control)" no-store
else
  code_now="$(jq -r .error.code <"$TMP/health.body" 2>/dev/null)"
  case "$code_now" in
    proxy_misconfigured) echo "        -> PHP-прокси не настроен: залейте api/config.php (deploy-front.sh --upload-config) и проверьте curl/secret";;
    upstream_unavailable|upstream_timeout) echo "        -> прокси не достучался до Go API: проверьте upstream в config.php, DNS/TLS и что kupol запущен на VPS";;
    bad_proxy_signature) echo "        -> Go отклонил подпись: секрет в config.php не совпадает с KUPOL_PROXY_SECRET на VPS или рассинхрон часов";;
  esac
  echo "        тело ответа: $(head -c 300 "$TMP/health.body")"
fi

fetch sess "$BASE/api/auth/session"
check "GET /api/auth/session (гость) -> 200" "$(code sess)" 200
check "гость: user = null" "$(jq -r '.user' <"$TMP/sess.body" 2>/dev/null)" null
[ -z "$(hdr sess set-cookie)" ] && ok "гостю куки не ставятся" || fail "гостю поставлена кука: $(hdr sess set-cookie)"

# Пустая строка запроса: Apache отдаёт PHP REQUEST_URI с «?», до Go запрос доходит без него — подпись должна покрывать
# именно то, что дошло (иначе 401 bad_proxy_signature на любой адрес вида «/api/...?»)
fetch emptyq "$BASE/api/auth/session?"
check "запрос с пустой строкой запроса («/api/auth/session?») -> 200" "$(code emptyq)" 200

fetch csrf -X POST -H 'Origin: https://evil.example' -H 'Content-Type: application/json' -d '{}' "$BASE/api/auth/logout"
check "чужой Origin (защита от CSRF; Origin дошёл до Go через прокси) -> 403" "$(code csrf)" 403
check "код ошибки" "$(jq -r .error.code <"$TMP/csrf.body" 2>/dev/null)" forbidden_origin

fetch nf "$BASE/api/no-such-thing"
check "GET /api/no-such-thing -> 404" "$(code nf)" 404
check "404 в формате API (error.code)" "$(jq -r .error.code <"$TMP/nf.body" 2>/dev/null)" not_found

fetch post -X POST "$BASE/api/health"
check "POST /api/health -> 405" "$(code post)" 405
check "405 в формате API (error.code)" "$(jq -r .error.code <"$TMP/post.body" 2>/dev/null)" method_not_allowed

fetch cookie -H 'Cookie: probe=1' "$BASE/api/health"
check "запрос с Cookie проходит через прокси" "$(code cookie)" 200

echo
if [ "$FAILS" -ne 0 ]; then echo "ИТОГ: критичных проблем — $FAILS, предупреждений — $WARNS"; exit 1; fi
echo "ИТОГ: всё в порядке (предупреждений: $WARNS)"
