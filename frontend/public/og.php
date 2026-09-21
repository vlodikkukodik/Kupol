<?php
/**
 * КУПОЛ — предпросмотр ссылок в мессенджерах и соцсетях.
 *
 * Apache (public/.htaccess) отдаёт сюда только запросы /doc/<шифр> от известных ботов предпросмотра (Telegram, WhatsApp, VK, Slack,
 * Discord, поисковики и т.п.). Люди получают обычное SPA. Скрипт берёт документ у Go API КАК ГОСТЬ (уровень 0): сервер уже отдаёт
 * гостю только открытое, поэтому в og:-теги попадает лишь то, что и так видно всем. Закрытый, несуществующий, неопубликованный документ
 * и любой сбой дают ту же страницу index.html без изменений — по ответу нельзя узнать, есть ли такой документ.
 */

declare(strict_types=1);

require __DIR__ . '/api/lib.php';

use function Kupol\Proxy\{client_ip, inject_og, load_config, og_from_document, pick_lang, proxy_message, sign, site_origin, valid_doc_ref};
use const Kupol\Proxy\{HEADER_IP, HEADER_SIG, HEADER_TS};

ini_set('display_errors', '0');
header_remove('X-Powered-By');

/** Отдаёт страницу и завершает работу. */
function page(string $html): never
{
    header('Content-Type: text/html; charset=utf-8');
    header('Cache-Control: no-cache');
    header('X-Content-Type-Options: nosniff');
    echo $html;
    exit;
}

// Язык предпросмотра: ?lang=it в ссылке (главнее) или Accept-Language бота; по умолчанию русский
$lang = pick_lang($_SERVER['HTTP_ACCEPT_LANGUAGE'] ?? null, is_string($_GET['lang'] ?? null) ? $_GET['lang'] : null);

$index = @file_get_contents(__DIR__ . '/index.html');
if ($index === false) {
    http_response_code(500);
    header('Content-Type: text/plain; charset=utf-8');
    echo proxy_message('Сбой архива', $lang);
    exit;
}

$ref = (string)($_GET['ref'] ?? '');
if (!valid_doc_ref($ref)) {
    page($index);
}

try {
    $config = load_config(__DIR__ . '/api');
    $ip = client_ip($_SERVER, $config);
    $uri = '/api/documents/' . rawurlencode($ref);
    $ts = time();
    $ch = curl_init($config['upstream'] . $uri);
    if ($ch === false) {
        page($index);
    }
    curl_setopt_array($ch, [
        CURLOPT_HTTPHEADER     => [
            'Accept: application/json',
            'Accept-Language: ' . $lang, // названия типов документа приходят на языке предпросмотра
            HEADER_IP . ': ' . $ip,
            HEADER_TS . ': ' . $ts,
            HEADER_SIG . ': ' . sign($config['secret'], $ts, $ip, 'GET', $uri),
            'User-Agent: kupol-og/1',
        ],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_FOLLOWLOCATION => false,
        CURLOPT_SSL_VERIFYPEER => true,
        CURLOPT_SSL_VERIFYHOST => 2,
        CURLOPT_CONNECTTIMEOUT => 2, // боты ждут недолго
        CURLOPT_TIMEOUT        => 5,
        CURLOPT_PROTOCOLS      => CURLPROTO_HTTP | CURLPROTO_HTTPS,
    ]);
    $body = curl_exec($ch);
    $status = (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    if (!is_string($body) || $status !== 200) {
        page($index);
    }
    $data = json_decode($body, true);
    $meta = is_array($data) ? og_from_document($data, site_origin($_SERVER), $lang) : null;
    page($meta === null ? $index : inject_og($index, $meta, $lang));
} catch (Throwable $e) {
    error_log('[kupol-og] ' . $e->getMessage());
    page($index);
}
