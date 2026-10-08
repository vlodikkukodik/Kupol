<?php
// Веб-клиент баз данных Vladhost (db.vladinc.ru): Adminer с входом по одноразовой ссылке из панели.
//
// 1. Панель выдаёт ссылку https://db.vladinc.ru/?vhtoken=…; здесь токен меняется на временную учётную запись
//    (POST на служебный адрес панели, только с этого сервера и с общим секретом), и Adminer получает готовый вход.
// 2. Adminer умеет подключаться к любому серверу, а значит, с этого сервера — к чужим базам соседних проектов.
//    Поэтому допустимы только два адреса из config.php: наши PostgreSQL и MariaDB.
// Сам Adminer лежит рядом (adminer.php, версия и контрольная сумма закреплены в prepare-db.sh) и не правится.

declare(strict_types=1);

$cfg = require __DIR__ . '/config.php';

function vh_stop(int $code, string $text): void
{
    http_response_code($code);
    header('Content-Type: text/plain; charset=utf-8');
    header('Cache-Control: no-store');
    echo $text;
    exit;
}

header('X-Content-Type-Options: nosniff');
header('X-Frame-Options: DENY');
header('Referrer-Policy: no-referrer');

// Драйвер Adminer → допустимый сервер. У PostgreSQL параметр адреса называется pgsql, у MySQL/MariaDB — server.
$servers = $cfg['servers'];

if (isset($_GET['vhtoken'])) {
    $token = (string) $_GET['vhtoken'];
    if (!preg_match('/^[A-Za-z0-9_-]{16,128}$/', $token)) {
        vh_stop(400, "Ссылка недействительна. Откройте базу заново из панели.\nIl link non è valido. Apri di nuovo il database dal pannello.\n");
    }
    $ch = curl_init($cfg['api']);
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => json_encode(['token' => $token]),
        CURLOPT_HTTPHEADER => ['Content-Type: application/json', 'X-Vh-Internal: ' . $cfg['key']],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_CONNECTTIMEOUT => 3,
        CURLOPT_TIMEOUT => 8,
    ]);
    $body = curl_exec($ch);
    $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    $sess = $status === 200 && is_string($body) ? json_decode($body, true) : null;
    if (!is_array($sess) || !isset($sess['driver'], $sess['server'], $sess['username'], $sess['password'], $sess['db'])
        || ($servers[$sess['driver']] ?? null) !== $sess['server']) {
        vh_stop(410, "Ссылка устарела или уже использована. Откройте базу заново из панели.\nIl link è scaduto o già usato. Apri di nuovo il database dal pannello.\n");
    }
    // Готовый вход: Adminer сам заведёт сессию и перенаправит на страницу базы (адрес уже без токена).
    // Adminer строит адрес перенаправления из REQUEST_URI: без очистки токен ушёл бы в него, и вторая загрузка получила бы «ссылка использована».
    unset($_GET['vhtoken']);
    $_SERVER['REQUEST_URI'] = '/';
    $_SERVER['QUERY_STRING'] = '';
    $_SERVER['REQUEST_METHOD'] = 'POST';
    $_POST = ['auth' => [
        'driver' => (string) $sess['driver'],
        'server' => (string) $sess['server'],
        'username' => (string) $sess['username'],
        'password' => (string) $sess['password'],
        'db' => (string) $sess['db'],
    ]];
    $_REQUEST = $_POST + $_GET;
}

// Ручной вход (форма Adminer) допустим только к нашим серверам.
$auth = $_POST['auth'] ?? null;
if (is_array($auth)) {
    $driver = (string) ($auth['driver'] ?? '');
    if (($servers[$driver] ?? null) !== (string) ($auth['server'] ?? '')) {
        vh_stop(403, "Разрешены только базы этого хостинга: откройте их из панели.\nSono consentiti solo i database di questo hosting: aprili dal pannello.\n");
    }
}
foreach (['pgsql', 'server', 'sqlite', 'sqlite2', 'mssql', 'oracle', 'elastic', 'mongo', 'clickhouse', 'simpledb'] as $driver) {
    if (isset($_GET[$driver]) && (!isset($servers[$driver]) || $_GET[$driver] !== $servers[$driver])) {
        vh_stop(403, "Разрешены только базы этого хостинга: откройте их из панели.\nSono consentiti solo i database di questo hosting: aprili dal pannello.\n");
    }
}

function adminer_object()
{
    class VladhostAdminer extends Adminer
    {
        public function name()
        {
            return 'Vladhost';
        }

        // Без «запомнить пароль»: временная учётная запись живёт недолго, постоянные входы не нужны.
        public function permanentLogin($create = false)
        {
            return '';
        }
    }

    return new VladhostAdminer();
}

require __DIR__ . '/adminer.php';
