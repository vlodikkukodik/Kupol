<?php
/**
 * Тестовый upstream для проверки PHP-прокси (запускается `php -S` из run.php).
 * Настоящий Go API проверяется отдельно: scripts/e2e.sh.
 */

declare(strict_types=1);

require __DIR__ . '/../../public/api/lib.php';

use function Kupol\Proxy\{decode_secret, sign};

$uri = $_SERVER['REQUEST_URI'];
$path = parse_url($uri, PHP_URL_PATH);

if (preg_match('#^/api/status/(\d{3})$#', $path, $m)) {
    http_response_code((int)$m[1]);
    if ($m[1] === '302') {
        header('Location: https://example.invalid/elsewhere');
    }
    if ($m[1] !== '204') {
        header('Content-Type: text/plain; charset=utf-8');
        echo 'status ' . $m[1];
    }
    return;
}

switch ($path) {
    case '/api/echo':
        $secret = decode_secret((string)getenv('KUPOL_PROXY_SECRET'));
        $ip = $_SERVER['HTTP_X_KUPOL_CLIENT_IP'] ?? '';
        $ts = (int)($_SERVER['HTTP_X_KUPOL_TIMESTAMP'] ?? 0);
        $sig = $_SERVER['HTTP_X_KUPOL_SIGNATURE'] ?? '';
        $expected = sign($secret, $ts, $ip, $_SERVER['REQUEST_METHOD'], $uri);
        $headers = [];
        foreach ($_SERVER as $k => $v) {
            if (str_starts_with($k, 'HTTP_') || $k === 'CONTENT_TYPE') {
                $headers[$k] = $v;
            }
        }
        header('Content-Type: application/json; charset=utf-8');
        header('X-Request-Id: ' . ($_SERVER['HTTP_X_REQUEST_ID'] ?? ''));
        header('X-Secret-Internal: must-not-leak');
        echo json_encode([
            'method'          => $_SERVER['REQUEST_METHOD'],
            'uri'             => $uri,
            'headers'         => $headers,
            'body_b64'        => base64_encode((string)file_get_contents('php://input')),
            'signature_valid' => hash_equals($expected, $sig),
            'ts_skew'         => abs(time() - $ts),
        ], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
        return;

    case '/api/cookies':
        header('Content-Type: application/json; charset=utf-8');
        header('Set-Cookie: kupol_session=abc123; Path=/; HttpOnly; Secure; SameSite=Lax');
        header('Set-Cookie: kupol_flag=1; Path=/; Max-Age=60', false);
        echo json_encode(['cookie' => $_SERVER['HTTP_COOKIE'] ?? null]);
        return;

    case '/api/binary':
        $blob = '';
        for ($i = 0; $i < 4096; $i++) {
            $blob .= hash('sha256', (string)$i, true); // 32 байта * 4096 = 128 КиБ, включая \0 и \xFF
        }
        header('Content-Type: application/pdf');
        header('Content-Disposition: attachment; filename="doc.pdf"');
        header('ETag: "fixture-1"');
        echo $blob;
        return;

    case '/api/slow':
        sleep(4);
        echo 'late';
        return;

    case '/api/head':
        header('Content-Type: text/plain; charset=utf-8');
        header('X-Fixture: head');
        echo 'body that HEAD must not carry';
        return;
}

http_response_code(404);
echo 'fixture: not found';
