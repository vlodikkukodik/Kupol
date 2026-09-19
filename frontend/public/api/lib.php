<?php
/**
 * КУПОЛ — PHP-прокси: чистые функции (без сетевого ввода-вывода).
 *
 * Совместимо с PHP 8.3. Формат подписи обязан совпадать с
 * backend/internal/proxyauth/proxyauth.go.
 */

declare(strict_types=1);

namespace Kupol\Proxy;

const SIGNATURE_PREFIX = 'KUPOL-PROXY-V1';
const MIN_SECRET_BYTES = 32;
/** Лимит тела запроса: совпадает с MaxBodyBytes в Go API (1 МиБ). */
const MAX_BODY_BYTES = 1048576;

const HEADER_IP = 'X-Kupol-Client-Ip';
const HEADER_TS = 'X-Kupol-Timestamp';
const HEADER_SIG = 'X-Kupol-Signature';

const ALLOWED_METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'];

/** $_SERVER-ключ => имя заголовка, который уходит на Go API. */
const FORWARD_REQUEST_HEADERS = [
    'CONTENT_TYPE'         => 'Content-Type',
    'HTTP_ACCEPT'          => 'Accept',
    'HTTP_ACCEPT_LANGUAGE' => 'Accept-Language',
    'HTTP_COOKIE'          => 'Cookie',
    'HTTP_USER_AGENT'      => 'User-Agent',
    'HTTP_ORIGIN'          => 'Origin',
    'HTTP_AUTHORIZATION'   => 'Authorization',
];

/** Заголовки ответа Go API, которые попадают к браузеру. */
const FORWARD_RESPONSE_HEADERS = [
    'content-type', 'content-disposition', 'content-language', 'cache-control',
    'etag', 'last-modified', 'retry-after', 'vary', 'set-cookie', 'x-request-id',
    'x-content-type-options', 'referrer-policy', 'location',
];

/**
 * Секрет в hex или base64 (std/url, с паддингом или без) — как в Go.
 *
 * @throws \InvalidArgumentException
 */
function decode_secret(string $value): string
{
    $value = trim($value);
    if ($value === '') {
        throw new \InvalidArgumentException('secret не задан');
    }

    $raw = null;
    if (strlen($value) % 2 === 0 && ctype_xdigit($value)) {
        $raw = hex2bin($value);
    } else {
        $b64 = strtr($value, '-_', '+/');
        $b64 = rtrim($b64, '=');
        if ($b64 !== '' && preg_match('/^[A-Za-z0-9+\/]+$/', $b64) === 1 && strlen($b64) % 4 !== 1) {
            $padded = $b64 . str_repeat('=', (4 - strlen($b64) % 4) % 4);
            $decoded = base64_decode($padded, true);
            if ($decoded !== false) {
                $raw = $decoded;
            }
        }
    }
    if ($raw === null || $raw === false) {
        throw new \InvalidArgumentException('secret не hex и не base64');
    }
    if (strlen($raw) < MIN_SECRET_BYTES) {
        throw new \InvalidArgumentException(
            sprintf('secret слишком короткий: %d байт, нужно не меньше %d', strlen($raw), MIN_SECRET_BYTES)
        );
    }
    return $raw;
}

/** HMAC-SHA256 в hex; строка для подписи — как в proxyauth.canonical. */
function sign(string $secret, int $ts, string $ip, string $method, string $requestUri): string
{
    $data = SIGNATURE_PREFIX . "\n" . $ts . "\n" . $ip . "\n" . strtoupper($method) . "\n" . $requestUri;
    return hash_hmac('sha256', $data, $secret);
}

/**
 * Собирает и проверяет конфигурацию.
 *
 * @param array<string,mixed> $file  содержимое config.php (или [] если файла нет)
 * @param array<string,string> $env  переменные окружения KUPOL_PROXY_* (для dev)
 * @return array{upstream:string,secret:string,ip_header:?string,connect_timeout:int,timeout:int}
 * @throws \InvalidArgumentException
 */
function build_config(array $file, array $env): array
{
    $pick = static function (string $fileKey, string $envKey) use ($file, $env) {
        if (array_key_exists($fileKey, $file) && $file[$fileKey] !== null && $file[$fileKey] !== '') {
            return $file[$fileKey];
        }
        $v = $env[$envKey] ?? '';
        return $v === '' ? null : $v;
    };

    $upstream = $pick('upstream', 'KUPOL_PROXY_UPSTREAM');
    if (!is_string($upstream)) {
        throw new \InvalidArgumentException('upstream не задан');
    }
    $upstream = rtrim($upstream, '/');
    $parts = parse_url($upstream);
    if ($parts === false || !isset($parts['scheme'], $parts['host'])
        || isset($parts['path']) || isset($parts['query']) || isset($parts['fragment'])
        || isset($parts['user']) || isset($parts['pass'])) {
        throw new \InvalidArgumentException('upstream должен быть вида https://host[:port] без пути и учётных данных');
    }
    $scheme = strtolower($parts['scheme']);
    $host = strtolower(trim($parts['host'], '[]'));
    $loopback = in_array($host, ['localhost', '127.0.0.1', '::1'], true);
    if ($scheme !== 'https' && !($scheme === 'http' && $loopback)) {
        throw new \InvalidArgumentException('upstream: http допустим только для localhost, иначе нужен https');
    }

    $secret = $pick('secret', 'KUPOL_PROXY_SECRET');
    if (!is_string($secret)) {
        throw new \InvalidArgumentException('secret не задан');
    }

    $ipHeader = $pick('ip_header', 'KUPOL_PROXY_IP_HEADER');
    if ($ipHeader !== null) {
        if (!is_string($ipHeader) || preg_match('/^HTTP_[A-Z0-9_]+$/', $ipHeader) !== 1) {
            throw new \InvalidArgumentException('ip_header должен быть ключом $_SERVER вида HTTP_X_REAL_IP');
        }
    }

    $connect = (int)($pick('connect_timeout', 'KUPOL_PROXY_CONNECT_TIMEOUT') ?? 5);
    $timeout = (int)($pick('timeout', 'KUPOL_PROXY_TIMEOUT') ?? 30);
    if ($connect < 1 || $timeout < $connect) {
        throw new \InvalidArgumentException('таймауты: connect_timeout >= 1 и timeout >= connect_timeout');
    }

    return [
        'upstream'        => $upstream,
        'secret'          => decode_secret($secret),
        'ip_header'       => $ipHeader,
        'connect_timeout' => $connect,
        'timeout'         => $timeout,
    ];
}

/**
 * IP клиента. По умолчанию REMOTE_ADDR. Если хостинг сам стоит за обратным прокси,
 * в config задаётся ip_header; берётся ПОСЛЕДНИЙ элемент списка — тот, что дописал
 * доверенный прокси хостинга (левые элементы клиент может подделать).
 *
 * @param array<string,mixed> $server
 * @param array{ip_header:?string} $config
 * @throws \RuntimeException
 */
function client_ip(array $server, array $config): string
{
    $candidate = $server['REMOTE_ADDR'] ?? '';
    if ($config['ip_header'] !== null) {
        $hdr = (string)($server[$config['ip_header']] ?? '');
        if ($hdr !== '') {
            $items = array_map('trim', explode(',', $hdr));
            $candidate = end($items);
        }
    }
    if (!is_string($candidate) || filter_var($candidate, FILTER_VALIDATE_IP) === false) {
        throw new \RuntimeException('не удалось определить IP клиента');
    }
    return $candidate;
}

/** Путь запроса безопасен для склейки с upstream: только /api/..., без управляющих символов. */
function valid_request_uri(string $uri): bool
{
    if (!str_starts_with($uri, '/api/') && $uri !== '/api') {
        return false;
    }
    // печатаемый ASCII без пробела: небезопасное должно быть %-закодировано
    return preg_match('/^[\x21-\x7E]+$/', $uri) === 1;
}

/**
 * Пустая строка запроса («/api/x?») уходит на upstream без «?»: curl отбрасывает пустой запрос, а Apache отдаёт PHP
 * REQUEST_URI с хвостовым «?». Подпись должна покрывать ровно то, что получит Go, — иначе он отклонит запрос как
 * подделанный (401 bad_proxy_signature). Поэтому и подписывается, и отправляется одинаково нормализованный URI.
 */
function normalize_request_uri(string $uri): string
{
    return str_ends_with($uri, '?') ? substr($uri, 0, -1) : $uri;
}

/**
 * Заголовки запроса для Go API (без служебных подписи — они добавляются отдельно).
 *
 * @param array<string,mixed> $server
 * @return list<string>  строки "Имя: значение"
 */
function forward_request_headers(array $server): array
{
    $out = [];
    foreach (FORWARD_REQUEST_HEADERS as $key => $name) {
        $value = $server[$key] ?? null;
        // Apache (CGI/FastCGI) кладёт Authorization сюда после RewriteRule в .htaccess
        if ($value === null && $key === 'HTTP_AUTHORIZATION') {
            $value = $server['REDIRECT_HTTP_AUTHORIZATION'] ?? null;
        }
        if (!is_string($value) || $value === '' || preg_match('/[\r\n\0]/', $value) === 1) {
            continue;
        }
        $out[] = $name . ': ' . $value;
    }
    return $out;
}

function valid_request_id(string $id): bool
{
    return preg_match('/^[A-Za-z0-9-]{8,64}$/', $id) === 1;
}

/** Заголовок ответа Go API пропускается к браузеру? */
function should_forward_response_header(string $name): bool
{
    return in_array(strtolower($name), FORWARD_RESPONSE_HEADERS, true);
}

/**
 * Разбор строки заголовка ответа.
 *
 * @return array{0:string,1:string}|null  [имя, значение]; null для статусной и пустой строк
 */
function parse_response_header_line(string $line): ?array
{
    $line = rtrim($line, "\r\n");
    if ($line === '' || str_starts_with($line, 'HTTP/')) {
        return null;
    }
    $pos = strpos($line, ':');
    if ($pos === false || $pos === 0) {
        return null;
    }
    return [substr($line, 0, $pos), trim(substr($line, $pos + 1))];
}

/** Код статуса из строки "HTTP/1.1 200 OK", иначе null. */
function parse_status_line(string $line): ?int
{
    if (preg_match('#^HTTP/\d(?:\.\d)?\s+(\d{3})#', $line, $m) === 1) {
        return (int)$m[1];
    }
    return null;
}

/**
 * Заголовок Server-Timing: сколько ушло на PHP-прокси и сколько на Go API (до первого байта ответа).
 * Видно во вкладке «Сеть» браузера — сразу ясно, где медленно: хостинг, канал до VPS или сам API.
 *
 * @param float $elapsedMs        сколько прошло от начала работы скрипта до момента отправки заголовков
 * @param float $upstreamSeconds  время до первого байта от Go API (CURLINFO_STARTTRANSFER_TIME)
 */
function server_timing(float $elapsedMs, float $upstreamSeconds): string
{
    $upstream = max(0.0, $upstreamSeconds * 1000.0);
    $proxy = max(0.0, $elapsedMs - $upstream);
    return sprintf('proxy;dur=%.1f;desc="PHP-прокси", upstream;dur=%.1f;desc="Go API до первого байта"', $proxy, $upstream);
}

/** Тело собственной ошибки прокси в том же формате, что и Go API. */
function error_body(string $code, string $message, string $requestId): string
{
    return json_encode(
        ['error' => ['code' => $code, 'message' => $message, 'request_id' => $requestId]],
        JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR
    );
}
