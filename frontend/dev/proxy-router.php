<?php
/**
 * Роутер для `php -S`: повторяет то, что на хостинге делает public/api/.htaccess —
 * всё под /api уходит в PHP-прокси. Используется в dev (Vite -> php -S -> Go)
 * и в тестах прокси, чтобы dev и прод шли одним и тем же кодом.
 */

declare(strict_types=1);

$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH) ?: '/';
if ($path === '/api' || str_starts_with($path, '/api/')) {
    require __DIR__ . '/../public/api/index.php';
    return true;
}
http_response_code(404);
header('Content-Type: text/plain; charset=utf-8');
echo "Только /api/*: статику отдаёт Vite\n";
return true;
