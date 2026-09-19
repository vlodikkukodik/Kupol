<?php
/**
 * Тесты PHP-прокси. Запуск: php frontend/tests/php/run.php  (или make test-proxy).
 *
 * Часть 1 — чистые функции lib.php.
 * Часть 2 — настоящий прокси (php -S + dev/proxy-router.php) против тестового
 * upstream (fixture-upstream.php): подпись, куки, статусы, бинарные данные,
 * таймауты, ошибки. Сеть — только loopback.
 */

declare(strict_types=1);

require __DIR__ . '/../../public/api/lib.php';

use function Kupol\Proxy\{build_config, client_ip, decode_secret, error_body, forward_request_headers, normalize_request_uri,
    parse_response_header_line, parse_status_line, server_timing, should_forward_response_header, sign,
    valid_request_id, valid_request_uri};

putenv('XDEBUG_MODE=off');

$passed = 0;
$failed = [];

function check(string $name, callable $fn): void
{
    global $passed, $failed;
    try {
        $fn();
        $passed++;
        echo "  ok   $name\n";
    } catch (Throwable $e) {
        $failed[] = $name;
        echo "  FAIL $name\n       " . str_replace("\n", "\n       ", $e->getMessage()) . "\n";
    }
}

function eq(mixed $actual, mixed $expected, string $what = ''): void
{
    if ($actual !== $expected) {
        throw new RuntimeException(
            ($what !== '' ? "$what: " : '') . 'ожидалось ' . var_export($expected, true) . ', получено ' . var_export($actual, true)
        );
    }
}

function truthy(mixed $v, string $what): void
{
    if (!$v) {
        throw new RuntimeException("$what: ожидалось истинное значение");
    }
}

function throws(callable $fn, string $class = InvalidArgumentException::class, string $contains = ''): void
{
    try {
        $fn();
    } catch (Throwable $e) {
        if (!($e instanceof $class)) {
            throw new RuntimeException('другое исключение: ' . get_class($e) . ' ' . $e->getMessage());
        }
        if ($contains !== '' && !str_contains($e->getMessage(), $contains)) {
            throw new RuntimeException("в сообщении «{$e->getMessage()}» нет «$contains»");
        }
        return;
    }
    throw new RuntimeException('исключение не выброшено');
}

// ---------------------------------------------------------------- часть 1
echo "lib.php\n";

$key = '0123456789abcdef0123456789abcdef';

check('server_timing: время прокси = всё время минус время Go; отрицательного не бывает; десятичная точка', function () {
    eq(server_timing(12.5, 0.0085), 'proxy;dur=4.0;desc="PHP-прокси", upstream;dur=8.5;desc="Go API до первого байта"');
    eq(server_timing(3.0, 0.010), 'proxy;dur=0.0;desc="PHP-прокси", upstream;dur=10.0;desc="Go API до первого байта"', 'upstream не больше общего');
    eq(server_timing(5.0, -1.0), 'proxy;dur=5.0;desc="PHP-прокси", upstream;dur=0.0;desc="Go API до первого байта"', 'мусор из curl');
    // при любой локали число пишется с точкой (sprintf %.1f зависит от LC_NUMERIC только для %f, но проверим явно)
    truthy(preg_match('/^proxy;dur=\d+\.\d;/', server_timing(1234.56, 0.1)) === 1, 'формат');
});

check('подпись: эталонный вектор совпадает с Go', function () use ($key) {
    // тот же вектор в backend/internal/proxyauth/proxyauth_test.go (получен через openssl)
    eq(
        sign($key, 1800000000, '203.0.113.7', 'get', '/api/health?x=1'),
        'cdc60051dec3164a78e4c4620cb720255b57da5068f1f8e269793c0310e9b777'
    );
});

check('подпись: метод приводится к верхнему регистру', function () use ($key) {
    eq(sign($key, 1, '1.1.1.1', 'post', '/api/x'), sign($key, 1, '1.1.1.1', 'POST', '/api/x'));
});

check('подпись: зависит от каждого поля', function () use ($key) {
    $base = sign($key, 1, '1.1.1.1', 'GET', '/api/x');
    truthy($base !== sign($key, 2, '1.1.1.1', 'GET', '/api/x'), 'ts');
    truthy($base !== sign($key, 1, '1.1.1.2', 'GET', '/api/x'), 'ip');
    truthy($base !== sign($key, 1, '1.1.1.1', 'POST', '/api/x'), 'method');
    truthy($base !== sign($key, 1, '1.1.1.1', 'GET', '/api/y'), 'uri');
    truthy($base !== sign('another-secret-another-secret-1234', 1, '1.1.1.1', 'GET', '/api/x'), 'secret');
});

check('decode_secret: hex, base64 (все варианты)', function () {
    $raw = '';
    for ($i = 1; $i <= 40; $i++) {
        $raw .= chr($i);
    }
    eq(decode_secret(bin2hex($raw)), $raw, 'hex');
    eq(decode_secret(base64_encode($raw)), $raw, 'std');
    eq(decode_secret(rtrim(base64_encode($raw), '=')), $raw, 'std без паддинга');
    eq(decode_secret(strtr(base64_encode($raw), '+/', '-_')), $raw, 'url');
    eq(decode_secret(rtrim(strtr(base64_encode($raw), '+/', '-_'), '=')), $raw, 'url без паддинга');
    eq(decode_secret("  " . bin2hex($raw) . "\n"), $raw, 'пробелы по краям');
});

check('decode_secret: отвергает короткие, пустые и мусор', function () {
    throws(fn() => decode_secret(''), InvalidArgumentException::class, 'не задан');
    throws(fn() => decode_secret(bin2hex(str_repeat('a', 31))), InvalidArgumentException::class, 'короткий');
    throws(fn() => decode_secret('не-секрет!'), InvalidArgumentException::class, 'не hex');
    throws(fn() => decode_secret('ЗАМЕНИТЬ_НА_СЕКРЕТ'), InvalidArgumentException::class);
});

$goodSecret = str_repeat('ab', 32);

check('build_config: минимальная валидная конфигурация', function () use ($goodSecret) {
    $c = build_config(['upstream' => 'https://api.kupol.vladinc.ru/', 'secret' => $goodSecret], []);
    eq($c['upstream'], 'https://api.kupol.vladinc.ru');
    eq(strlen($c['secret']), 32);
    eq($c['ip_header'], null);
    eq($c['connect_timeout'], 5);
    eq($c['timeout'], 30);
});

check('build_config: env как запасной источник, файл приоритетнее', function () use ($goodSecret) {
    $env = ['KUPOL_PROXY_UPSTREAM' => 'http://127.0.0.1:8080', 'KUPOL_PROXY_SECRET' => $goodSecret];
    eq(build_config([], $env)['upstream'], 'http://127.0.0.1:8080');
    eq(build_config(['upstream' => 'https://a.example'], $env)['upstream'], 'https://a.example');
});

check('build_config: http только для loopback', function () use ($goodSecret) {
    foreach (['http://localhost:8080', 'http://127.0.0.1:1', 'http://[::1]:9'] as $u) {
        build_config(['upstream' => $u, 'secret' => $goodSecret], []);
    }
    throws(fn() => build_config(['upstream' => 'http://api.kupol.vladinc.ru', 'secret' => $goodSecret], []), InvalidArgumentException::class, 'https');
});

check('build_config: отвергает плохой upstream, ip_header, таймауты', function () use ($goodSecret) {
    foreach (['', 'api.kupol.vladinc.ru', 'https://h/path', 'https://h?x=1', 'https://u:p@h', 'ftp://h'] as $u) {
        throws(fn() => build_config(['upstream' => $u, 'secret' => $goodSecret], []), InvalidArgumentException::class, '', );
    }
    throws(fn() => build_config(['upstream' => 'https://h', 'secret' => $goodSecret, 'ip_header' => 'X-Real-IP'], []));
    throws(fn() => build_config(['upstream' => 'https://h', 'secret' => $goodSecret, 'timeout' => 2, 'connect_timeout' => 5], []));
    throws(fn() => build_config(['upstream' => 'https://h'], []), InvalidArgumentException::class, 'secret');
});

check('client_ip: REMOTE_ADDR по умолчанию, IPv6 допустим', function () {
    $cfg = ['ip_header' => null];
    eq(client_ip(['REMOTE_ADDR' => '203.0.113.7'], $cfg), '203.0.113.7');
    eq(client_ip(['REMOTE_ADDR' => '2001:db8::1'], $cfg), '2001:db8::1');
    // без ip_header заголовки клиента игнорируются
    eq(client_ip(['REMOTE_ADDR' => '203.0.113.7', 'HTTP_X_FORWARDED_FOR' => '1.2.3.4'], $cfg), '203.0.113.7');
});

check('client_ip: ip_header берёт последний элемент (левые подделываются)', function () {
    $cfg = ['ip_header' => 'HTTP_X_FORWARDED_FOR'];
    eq(client_ip(['REMOTE_ADDR' => '10.0.0.1', 'HTTP_X_FORWARDED_FOR' => '6.6.6.6, 198.51.100.4'], $cfg), '198.51.100.4');
    eq(client_ip(['REMOTE_ADDR' => '10.0.0.1'], $cfg), '10.0.0.1', 'нет заголовка — REMOTE_ADDR');
});

check('client_ip: невалидный IP — ошибка', function () {
    throws(fn() => client_ip(['REMOTE_ADDR' => 'garbage'], ['ip_header' => null]), RuntimeException::class);
    throws(fn() => client_ip([], ['ip_header' => null]), RuntimeException::class);
    throws(fn() => client_ip(['REMOTE_ADDR' => '1.1.1.1', 'HTTP_X_REAL_IP' => 'x'], ['ip_header' => 'HTTP_X_REAL_IP']), RuntimeException::class);
});

check('valid_request_uri', function () {
    foreach (['/api/health', '/api/health?x=1&y=%D0%B0', '/api/a/b/c', '/api'] as $ok) {
        truthy(valid_request_uri($ok), $ok);
    }
    foreach (['/', '/apix', '/api2/health', '//api/health', 'api/health', "/api/x\r\nHost: evil", '/api/a b', "/api/\x00", '/api/привет', 'http://evil/api/x', ''] as $bad) {
        truthy(!valid_request_uri($bad), 'должен отвергаться: ' . json_encode($bad, JSON_UNESCAPED_UNICODE));
    }
});

check('normalize_request_uri: пустая строка запроса убирается, остальное — байт-в-байт', function () {
    // Apache отдаёт PHP REQUEST_URI с хвостовым «?», curl уходит без него: подпись должна покрывать то, что уйдёт
    eq(normalize_request_uri('/api/team/members?'), '/api/team/members');
    eq(normalize_request_uri('/api/x??'), '/api/x?', 'убирается только один знак');
    // непустой запрос, в том числе заканчивающийся «&» или «=», не трогается
    foreach (['/api/health', '/api/health?a=1', '/api/x?a=1&', '/api/x?a=', '/api/x?&', '/api/x?%3F', '/api/a?b=%D0%B0%D0%B1'] as $same) {
        eq(normalize_request_uri($same), $same, $same);
    }
    eq(normalize_request_uri(''), '');
    eq(normalize_request_uri('?'), '');
});

check('forward_request_headers: белый список и защита от CRLF', function () {
    $h = forward_request_headers([
        'HTTP_COOKIE'          => 'kupol_session=abc',
        'HTTP_ACCEPT'          => 'application/json',
        'HTTP_USER_AGENT'      => 'UA',
        'HTTP_ORIGIN'          => 'https://kupol.vladinc.ru',
        'CONTENT_TYPE'         => 'application/json',
        'HTTP_ACCEPT_LANGUAGE' => "ru\r\nX-Injected: 1",
        'HTTP_HOST'            => 'kupol.vladinc.ru',
        'HTTP_X_FORWARDED_FOR' => '6.6.6.6',
        'HTTP_X_KUPOL_CLIENT_IP' => '6.6.6.6',
        'HTTP_ACCEPT_ENCODING' => 'gzip',
    ]);
    sort($h);
    eq($h, [
        'Accept: application/json',
        'Content-Type: application/json',
        'Cookie: kupol_session=abc',
        'Origin: https://kupol.vladinc.ru',
        'User-Agent: UA',
    ]);
});

check('forward_request_headers: Authorization, в т.ч. через REDIRECT_', function () {
    eq(forward_request_headers(['HTTP_AUTHORIZATION' => 'Bearer k1']), ['Authorization: Bearer k1']);
    eq(forward_request_headers(['REDIRECT_HTTP_AUTHORIZATION' => 'Bearer k2']), ['Authorization: Bearer k2']);
});

check('valid_request_id', function () {
    truthy(valid_request_id('abcd1234'), 'ok');
    truthy(valid_request_id(str_repeat('a', 64)), '64');
    truthy(!valid_request_id('short'), 'короткий');
    truthy(!valid_request_id(str_repeat('a', 65)), 'длинный');
    truthy(!valid_request_id("abcd1234\r\nX: 1"), 'CRLF');
});

check('заголовки ответа: белый список, статусная строка, Set-Cookie', function () {
    truthy(should_forward_response_header('Set-Cookie'), 'set-cookie');
    truthy(should_forward_response_header('CONTENT-TYPE'), 'регистр');
    truthy(!should_forward_response_header('Server'), 'server');
    truthy(!should_forward_response_header('X-Secret-Internal'), 'внутренний');
    truthy(!should_forward_response_header('Transfer-Encoding'), 'hop-by-hop');
    truthy(!should_forward_response_header('Content-Length'), 'длину считает веб-сервер хостинга');
    eq(parse_response_header_line("Content-Type: text/plain\r\n"), ['Content-Type', 'text/plain']);
    eq(parse_response_header_line("Set-Cookie: a=b; Path=/\r\n"), ['Set-Cookie', 'a=b; Path=/']);
    eq(parse_response_header_line("HTTP/1.1 200 OK\r\n"), null);
    eq(parse_response_header_line("\r\n"), null);
    eq(parse_response_header_line(": nameless"), null);
    eq(parse_status_line("HTTP/1.1 204 No Content\r\n"), 204);
    eq(parse_status_line("HTTP/2 200\r\n"), 200);
    eq(parse_status_line("Content-Type: x"), null);
});

check('error_body: формат совпадает с Go API', function () {
    $j = json_decode(error_body('upstream_timeout', 'Сбой архива', 'rid-12345678'), true);
    eq($j, ['error' => ['code' => 'upstream_timeout', 'message' => 'Сбой архива', 'request_id' => 'rid-12345678']]);
    truthy(str_contains(error_body('x', 'Сбой архива', 'r'), 'Сбой архива'), 'кириллица не экранируется');
});

// ---------------------------------------------------------------- часть 2
echo "\nпрокси через php -S\n";

/** @var list<resource> $procs */
$procs = [];

function free_port(): int
{
    $s = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
    if ($s === false) {
        throw new RuntimeException("нет свободного порта: $errstr");
    }
    $port = (int)explode(':', stream_socket_get_name($s, false))[1];
    fclose($s);
    return $port;
}

/**
 * @param array<string,string> $env
 * @return array{port:int,proc:resource}
 */
function start_server(string $router, array $env): array
{
    global $procs;
    $port = free_port();
    $cmd = [PHP_BINARY, '-d', 'display_errors=0', '-d', 'log_errors=1', '-d', 'error_log=/dev/null', '-S', "127.0.0.1:$port", $router];
    $proc = proc_open(
        $cmd,
        [0 => ['pipe', 'r'], 1 => ['file', '/dev/null', 'w'], 2 => ['file', '/dev/null', 'w']],
        $pipes,
        __DIR__,
        array_merge($env, ['XDEBUG_MODE' => 'off', 'PHP_CLI_SERVER_WORKERS' => '4', 'PATH' => getenv('PATH') ?: ''])
    );
    if (!is_resource($proc)) {
        throw new RuntimeException('не удалось запустить php -S');
    }
    $procs[] = $proc;
    for ($i = 0; $i < 100; $i++) {
        $c = @fsockopen('127.0.0.1', $port, $en, $es, 0.1);
        if ($c !== false) {
            fclose($c);
            return ['port' => $port, 'proc' => $proc];
        }
        usleep(50000);
    }
    throw new RuntimeException("php -S на порту $port не поднялся");
}

register_shutdown_function(function () use (&$procs) {
    foreach ($procs as $p) {
        if (is_resource($p)) {
            proc_terminate($p);
            proc_close($p);
        }
    }
});

/**
 * @param list<string> $headers
 * @return array{status:int,headers:list<string>,body:string,errno:int}
 */
function http(string $method, string $url, array $headers = [], ?string $body = null, int $timeout = 10): array
{
    $ch = curl_init($url);
    $raw = [];
    curl_setopt_array($ch, [
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_HTTPHEADER     => array_merge($headers, ['Expect:']),
        CURLOPT_FOLLOWLOCATION => false,
        CURLOPT_TIMEOUT        => $timeout,
        CURLOPT_HEADERFUNCTION => function ($c, $line) use (&$raw) {
            $raw[] = rtrim($line, "\r\n");
            return strlen($line);
        },
    ]);
    if ($method === 'HEAD') {
        curl_setopt($ch, CURLOPT_NOBODY, true);
    }
    if ($body !== null) {
        curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
    }
    $out = curl_exec($ch);
    $res = ['status' => (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE), 'headers' => $raw, 'body' => $out === false ? '' : $out, 'errno' => curl_errno($ch)];
    curl_close($ch);
    return $res;
}

/** Значения заголовка (без учёта регистра) из последнего блока ответа. */
function header_values(array $res, string $name): array
{
    $vals = [];
    foreach ($res['headers'] as $l) {
        if (stripos($l, $name . ':') === 0) {
            $vals[] = trim(substr($l, strlen($name) + 1));
        }
    }
    return $vals;
}

$secretHex = bin2hex(random_bytes(32));
$fixture = start_server(__DIR__ . '/fixture-upstream.php', ['KUPOL_PROXY_SECRET' => $secretHex]);
$router = __DIR__ . '/../../dev/proxy-router.php';
$proxyEnv = [
    'KUPOL_PROXY_UPSTREAM' => 'http://127.0.0.1:' . $fixture['port'],
    'KUPOL_PROXY_SECRET'   => $secretHex,
];
$proxy = start_server($router, $proxyEnv);
$P = 'http://127.0.0.1:' . $proxy['port'];

check('GET: подпись валидна, IP клиента подписан, URI передан байт-в-байт', function () use ($P) {
    $r = http('GET', "$P/api/echo?a=1&b=%D0%B0%D0%B1", ['Accept: application/json']);
    eq($r['status'], 200);
    $j = json_decode($r['body'], true);
    eq($j['signature_valid'], true, 'подпись');
    eq($j['uri'], '/api/echo?a=1&b=%D0%B0%D0%B1', 'uri');
    eq($j['method'], 'GET');
    eq($j['headers']['HTTP_X_KUPOL_CLIENT_IP'], '127.0.0.1', 'ip');
    truthy($j['ts_skew'] <= 2, 'ts');
});

check('пустая строка запроса («/api/echo?»): подпись сходится с тем, что дошло до Go', function () use ($P) {
    // Apache отдаёт PHP REQUEST_URI с хвостовым «?», а curl отправляет запрос без него: подписывать нужно именно то,
    // что уйдёт на upstream, иначе Go отклонит запрос как подделанный.
    foreach (['/api/echo?', '/api/echo?&', '/api/echo?a=1&'] as $path) {
        $r = http('GET', $P . $path, ['Accept: application/json']);
        eq($r['status'], 200, $path);
        $j = json_decode($r['body'], true);
        eq($j['signature_valid'], true, "подпись для $path");
    }
});

check('Server-Timing: есть у ответа, время Go не превышает общее; у ответов без тела (204) — тоже', function () use ($P) {
    foreach (['/api/echo', '/api/cookies'] as $path) {
        $r = http('GET', $P . $path);
        $vals = header_values($r, 'Server-Timing');
        eq(count($vals), 1, "заголовок у $path");
        truthy(preg_match('/^proxy;dur=(\d+\.\d);desc="PHP-прокси", upstream;dur=(\d+\.\d);desc="Go API до первого байта"$/u', $vals[0], $m) === 1, "формат: {$vals[0]}");
        truthy((float)$m[1] >= 0 && (float)$m[2] >= 0, 'неотрицательные значения');
    }
});

check('клиентские X-Kupol-* и X-Forwarded-For подделать нельзя', function () use ($P) {
    $r = http('GET', "$P/api/echo", [
        'X-Kupol-Client-Ip: 6.6.6.6', 'X-Kupol-Signature: deadbeef', 'X-Kupol-Timestamp: 1',
        'X-Forwarded-For: 6.6.6.6', 'X-Real-IP: 6.6.6.6',
    ]);
    $j = json_decode($r['body'], true);
    eq($j['signature_valid'], true, 'подпись настоящая');
    eq($j['headers']['HTTP_X_KUPOL_CLIENT_IP'], '127.0.0.1');
    truthy(!isset($j['headers']['HTTP_X_FORWARDED_FOR']), 'X-Forwarded-For не должен доходить');
    truthy(!isset($j['headers']['HTTP_X_REAL_IP']), 'X-Real-IP не должен доходить');
});

check('POST: тело с кириллицей и нулевыми байтами передаётся без искажений', function () use ($P) {
    $body = json_encode(['текст' => "Изделие К-19 — «под Куполом»", 'n' => 1], JSON_UNESCAPED_UNICODE);
    $r = http('POST', "$P/api/echo", ['Content-Type: application/json'], $body);
    $j = json_decode($r['body'], true);
    eq(base64_decode($j['body_b64'], true), $body, 'тело');
    eq($j['headers']['CONTENT_TYPE'], 'application/json');
    eq($j['signature_valid'], true);

    $bin = "a\0b\xFF\xFEc";
    $j = json_decode(http('PUT', "$P/api/echo", ['Content-Type: application/octet-stream'], $bin)['body'], true);
    eq(base64_decode($j['body_b64'], true), $bin, 'бинарное тело');
});

check('методы PUT/PATCH/DELETE проходят и подписываются с этим методом', function () use ($P) {
    foreach (['PUT', 'PATCH', 'DELETE'] as $m) {
        $j = json_decode(http($m, "$P/api/echo", ['Content-Type: application/json'], '{}')['body'], true);
        eq($j['method'], $m);
        eq($j['signature_valid'], true, $m);
    }
});

check('Cookie уходит на upstream, несколько Set-Cookie доходят раздельно', function () use ($P) {
    $r = http('GET', "$P/api/cookies", ['Cookie: kupol_session=old; theme=paper']);
    eq(json_decode($r['body'], true)['cookie'], 'kupol_session=old; theme=paper');
    $set = header_values($r, 'Set-Cookie');
    eq(count($set), 2, 'число Set-Cookie');
    eq($set[0], 'kupol_session=abc123; Path=/; HttpOnly; Secure; SameSite=Lax');
    eq($set[1], 'kupol_flag=1; Path=/; Max-Age=60');
});

check('статусы проходят как есть; редирект не преследуется', function () use ($P) {
    $r = http('GET', "$P/api/status/204");
    eq($r['status'], 204);
    eq($r['body'], '');
    $r = http('GET', "$P/api/status/404");
    eq([$r['status'], $r['body']], [404, 'status 404']);
    $r = http('GET', "$P/api/status/500");
    eq($r['status'], 500);
    $r = http('GET', "$P/api/status/302");
    eq($r['status'], 302);
    eq(header_values($r, 'Location'), ['https://example.invalid/elsewhere']);
});

check('бинарный ответ не искажается (128 КиБ), заголовки файла сохранены', function () use ($P) {
    $expected = '';
    for ($i = 0; $i < 4096; $i++) {
        $expected .= hash('sha256', (string)$i, true);
    }
    $r = http('GET', "$P/api/binary");
    eq(strlen($r['body']), strlen($expected), 'размер');
    eq(hash('sha256', $r['body']), hash('sha256', $expected), 'sha256');
    eq(header_values($r, 'Content-Type'), ['application/pdf']);
    eq(header_values($r, 'Content-Disposition'), ['attachment; filename="doc.pdf"']);
    eq(header_values($r, 'ETag'), ['"fixture-1"']);
});

check('HEAD: заголовки есть, тела нет', function () use ($P) {
    $r = http('HEAD', "$P/api/head");
    eq($r['status'], 200);
    eq($r['body'], '');
    eq(header_values($r, 'X-Fixture'), [], 'X-Fixture не в белом списке');
    eq(header_values($r, 'Content-Type'), ['text/plain; charset=utf-8']);
});

check('заголовки вне белого списка не доходят до браузера', function () use ($P) {
    $r = http('GET', "$P/api/echo");
    eq(header_values($r, 'X-Secret-Internal'), []);
    eq(header_values($r, 'X-Powered-By'), []);
});

check('X-Request-Id: валидный сохраняется, мусор заменяется, всегда есть', function () use ($P) {
    $r = http('GET', "$P/api/echo", ['X-Request-Id: my-req-12345']);
    eq(header_values($r, 'X-Request-Id'), ['my-req-12345']);
    eq(json_decode($r['body'], true)['headers']['HTTP_X_REQUEST_ID'], 'my-req-12345');

    $r = http('GET', "$P/api/echo", ['X-Request-Id: bad id!']);
    $id = header_values($r, 'X-Request-Id')[0] ?? '';
    truthy(preg_match('/^[a-f0-9]{16}$/', $id) === 1, "сгенерированный id: $id");
});

check('метод вне списка — 405 в формате API', function () use ($P) {
    foreach (['OPTIONS', 'TRACE', 'CONNECT'] as $m) {
        $r = http($m, "$P/api/echo");
        eq($r['status'], 405, $m);
        eq(json_decode($r['body'], true)['error']['code'], 'method_not_allowed', $m);
        eq(header_values($r, 'Allow'), ['GET, HEAD, POST, PUT, PATCH, DELETE'], "Allow $m");
    }
});

check('пути вне /api/ не уходят на upstream', function () use ($P) {
    // Роутер отдаёт 404 сам; сам index.php тоже отсекает такие URI (проверено в valid_request_uri)
    eq(http('GET', "$P/")['status'], 404);
    eq(http('GET', "$P/apix/echo")['status'], 404);
});

check('слишком большое тело — 413 без обращения к upstream', function () use ($P) {
    $r = http('POST', "$P/api/echo", ['Content-Type: text/plain'], str_repeat('x', 1048576 + 1));
    eq($r['status'], 413);
    eq(json_decode($r['body'], true)['error']['code'], 'payload_too_large');
    // ровно на границе — проходит
    $r = http('POST', "$P/api/echo", ['Content-Type: text/plain'], str_repeat('x', 1048576));
    eq($r['status'], 200);
});

check('upstream недоступен — 502 в формате API, без подробностей наружу', function () {
    $dead = free_port();
    $p = start_server(__DIR__ . '/../../dev/proxy-router.php', [
        'KUPOL_PROXY_UPSTREAM' => "http://127.0.0.1:$dead", 'KUPOL_PROXY_SECRET' => bin2hex(random_bytes(32)),
    ]);
    $r = http('GET', 'http://127.0.0.1:' . $p['port'] . '/api/health');
    eq($r['status'], 502);
    $j = json_decode($r['body'], true);
    eq($j['error']['code'], 'upstream_unavailable');
    eq($j['error']['message'], 'Сбой архива');
    truthy(!str_contains($r['body'], (string)$dead), 'адрес upstream не должен утекать');
    truthy($j['error']['request_id'] !== '', 'request_id');
    eq(header_values($r, 'X-Request-Id'), [$j['error']['request_id']]);
});

check('upstream отвечает дольше таймаута — 504', function () use ($secretHex, $fixture) {
    $p = start_server(__DIR__ . '/../../dev/proxy-router.php', [
        'KUPOL_PROXY_UPSTREAM' => 'http://127.0.0.1:' . $fixture['port'], 'KUPOL_PROXY_SECRET' => $secretHex,
        'KUPOL_PROXY_CONNECT_TIMEOUT' => '1', 'KUPOL_PROXY_TIMEOUT' => '1',
    ]);
    $t = microtime(true);
    $r = http('GET', 'http://127.0.0.1:' . $p['port'] . '/api/slow');
    eq($r['status'], 504);
    eq(json_decode($r['body'], true)['error']['code'], 'upstream_timeout');
    truthy(microtime(true) - $t < 3.5, 'должен оборваться по таймауту, а не ждать upstream');
});

check('ошибка конфигурации — 500 без раскрытия причины', function () {
    $p = start_server(__DIR__ . '/../../dev/proxy-router.php', ['KUPOL_PROXY_UPSTREAM' => 'http://127.0.0.1:1']);
    $r = http('GET', 'http://127.0.0.1:' . $p['port'] . '/api/health');
    eq($r['status'], 500);
    $j = json_decode($r['body'], true);
    eq($j['error']['code'], 'proxy_misconfigured');
    truthy(!str_contains($r['body'], 'secret'), 'причина не должна утекать в ответ');
});

check('ip_header из конфига: подписывается последний элемент списка', function () use ($secretHex, $fixture) {
    $p = start_server(__DIR__ . '/../../dev/proxy-router.php', [
        'KUPOL_PROXY_UPSTREAM' => 'http://127.0.0.1:' . $fixture['port'], 'KUPOL_PROXY_SECRET' => $secretHex,
        'KUPOL_PROXY_IP_HEADER' => 'HTTP_X_FORWARDED_FOR',
    ]);
    $j = json_decode(http('GET', 'http://127.0.0.1:' . $p['port'] . '/api/echo', ['X-Forwarded-For: 6.6.6.6, 198.51.100.4'])['body'], true);
    eq($j['headers']['HTTP_X_KUPOL_CLIENT_IP'], '198.51.100.4');
    eq($j['signature_valid'], true);
});

echo "\n";
if ($failed !== []) {
    echo count($failed) . " упало, $passed прошло:\n - " . implode("\n - ", $failed) . "\n";
    exit(1);
}
echo "Все тесты прошли: $passed\n";
