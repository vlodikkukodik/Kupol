<?php
/**
 * КУПОЛ — PHP-прокси на shared-хостинге.
 *
 * Все обычные запросы фронта (/api/...) идут сюда, отсюда — на Go API.
 * Прокси: ставит и подписывает реальный IP клиента, пересылает Cookie
 * и Set-Cookie, отдаёт ответ потоком. Никакой бизнес-логики здесь нет.
 *
 * Настройки — в config.php рядом (см. config.example.php) или в переменных
 * окружения KUPOL_PROXY_* (dev).
 */

declare(strict_types=1);

require __DIR__ . '/lib.php';

use function Kupol\Proxy\{load_config, client_ip, error_body, forward_request_headers, normalize_request_uri,
    parse_response_header_line, parse_status_line, server_timing, should_forward_response_header, sign,
    valid_request_id, valid_request_uri};
use const Kupol\Proxy\{ALLOWED_METHODS, HEADER_IP, HEADER_SIG, HEADER_TS, MAX_BODY_BYTES};

$startedAt = hrtime(true); // для Server-Timing

ini_set('display_errors', '0');
ini_set('zlib.output_compression', '0');
header_remove('X-Powered-By'); // не светить версию PHP
set_time_limit(0); // ограничиваем cURL-таймаутами, а не PHP
while (ob_get_level() > 0) {
    ob_end_clean();
}

/** Ответ об ошибке от имени прокси. */
function fail(int $status, string $code, string $message, string $requestId): never
{
    if (!headers_sent()) {
        http_response_code($status);
        header('Content-Type: application/json; charset=utf-8');
        header('Cache-Control: no-store');
        header('X-Request-Id: ' . $requestId);
        header('X-Content-Type-Options: nosniff');
    }
    echo error_body($code, $message, $requestId);
    exit;
}

$requestId = bin2hex(random_bytes(8));
$incomingId = $_SERVER['HTTP_X_REQUEST_ID'] ?? '';
if (is_string($incomingId) && valid_request_id($incomingId)) {
    $requestId = $incomingId;
}

// --- конфигурация ---------------------------------------------------------
try {
    $config = load_config(__DIR__);
} catch (Throwable $e) {
    error_log('[kupol-proxy] конфигурация: ' . $e->getMessage());
    fail(500, 'proxy_misconfigured', 'Сбой архива', $requestId);
}

// --- проверка запроса -----------------------------------------------------
$method = strtoupper((string)($_SERVER['REQUEST_METHOD'] ?? ''));
if (!in_array($method, ALLOWED_METHODS, true)) {
    header('Allow: ' . implode(', ', ALLOWED_METHODS));
    fail(405, 'method_not_allowed', 'Метод не поддерживается', $requestId);
}

$uri = (string)($_SERVER['REQUEST_URI'] ?? '');
if (!valid_request_uri($uri)) {
    fail(404, 'not_found', 'Дело не найдено', $requestId);
}
$uri = normalize_request_uri($uri);

$body = null;
if ($method !== 'GET' && $method !== 'HEAD') {
    $declared = (int)($_SERVER['CONTENT_LENGTH'] ?? 0);
    if ($declared > MAX_BODY_BYTES) {
        fail(413, 'payload_too_large', 'Слишком большой запрос', $requestId);
    }
    $in = fopen('php://input', 'rb');
    $body = $in === false ? '' : stream_get_contents($in, MAX_BODY_BYTES + 1);
    if ($in !== false) {
        fclose($in);
    }
    if ($body === false) {
        $body = '';
    }
    if (strlen($body) > MAX_BODY_BYTES) {
        fail(413, 'payload_too_large', 'Слишком большой запрос', $requestId);
    }
}

try {
    $ip = client_ip($_SERVER, $config);
} catch (RuntimeException $e) {
    error_log('[kupol-proxy] ' . $e->getMessage() . ' (' . $requestId . ')');
    fail(500, 'proxy_no_client_ip', 'Сбой архива', $requestId);
}

// --- запрос на Go API -----------------------------------------------------
$ts = time();
$headers = forward_request_headers($_SERVER);
$headers[] = 'X-Request-Id: ' . $requestId;
$headers[] = HEADER_IP . ': ' . $ip;
$headers[] = HEADER_TS . ': ' . $ts;
$headers[] = HEADER_SIG . ': ' . sign($config['secret'], $ts, $ip, $method, $uri);
$headers[] = 'Expect:'; // не ждать 100-continue

$ch = curl_init($config['upstream'] . $uri);
if ($ch === false) {
    error_log('[kupol-proxy] curl_init не удался (' . $requestId . ')');
    fail(500, 'proxy_curl', 'Сбой архива', $requestId);
}

$status = 0;
$pending = [];       // заголовки текущего блока ответа
$headersSent = false;
$upstreamTtfb = 0.0; // секунд от начала запроса к Go до первого байта ответа

$sendHead = static function () use (&$status, &$pending, &$headersSent, &$upstreamTtfb, $startedAt): void {
    if ($headersSent) {
        return;
    }
    $headersSent = true;
    http_response_code($status);
    foreach ($pending as [$name, $value]) {
        // Set-Cookie может быть несколько — не заменяем, а добавляем
        header($name . ': ' . $value, strtolower($name) !== 'set-cookie');
    }
    header('Server-Timing: ' . server_timing((hrtime(true) - $startedAt) / 1e6, $upstreamTtfb));
};

curl_setopt_array($ch, [
    CURLOPT_CUSTOMREQUEST  => $method,
    CURLOPT_HTTPHEADER     => $headers,
    CURLOPT_FOLLOWLOCATION => false,
    CURLOPT_SSL_VERIFYPEER => true,
    CURLOPT_SSL_VERIFYHOST => 2,
    CURLOPT_CONNECTTIMEOUT => $config['connect_timeout'],
    CURLOPT_TIMEOUT        => $config['timeout'],
    CURLOPT_HTTP_VERSION   => CURL_HTTP_VERSION_1_1,
    CURLOPT_PROTOCOLS      => CURLPROTO_HTTP | CURLPROTO_HTTPS,
    CURLOPT_HEADERFUNCTION => static function ($ch, string $line) use (&$status, &$pending, &$upstreamTtfb): int {
        $code = parse_status_line($line);
        if ($code !== null) {           // новый блок ответа (в т.ч. 1xx) — начинаем заново
            $status = $code;
            $pending = [];
            $upstreamTtfb = (float)curl_getinfo($ch, CURLINFO_STARTTRANSFER_TIME);
        } else {
            $parsed = parse_response_header_line($line);
            if ($parsed !== null && should_forward_response_header($parsed[0])) {
                $pending[] = $parsed;
            }
        }
        return strlen($line);
    },
    CURLOPT_WRITEFUNCTION  => static function ($ch, string $chunk) use ($sendHead): int {
        $sendHead();
        echo $chunk;
        flush();
        return strlen($chunk);
    },
]);
if ($method === 'HEAD') {
    curl_setopt($ch, CURLOPT_NOBODY, true);
}
if ($body !== null) {
    curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
}

$ok = curl_exec($ch);
$errno = curl_errno($ch);
$error = curl_error($ch);
curl_close($ch);

if ($ok === false || $errno !== 0) {
    error_log(sprintf('[kupol-proxy] upstream: %s (errno %d, %s %s, %s)', $error, $errno, $method, $uri, $requestId));
    if ($headersSent) {
        exit; // ответ уже пошёл клиенту — остаётся оборвать соединение
    }
    if ($errno === CURLE_OPERATION_TIMEDOUT) {
        fail(504, 'upstream_timeout', 'Сбой архива', $requestId);
    }
    fail(502, 'upstream_unavailable', 'Сбой архива', $requestId);
}

$sendHead(); // ответ без тела (204, 304, HEAD, пустой 200)
