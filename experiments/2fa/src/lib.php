<?php
declare(strict_types=1);

const ENV_FILE = __DIR__ . '/../.env';

/** Читает .env (KEY=VALUE, комментарии #, значения в кавычках). */
function env(string $key, ?string $default = null): string
{
    static $vars = null;
    if ($vars === null) {
        $vars = [];
        if (!is_readable(ENV_FILE)) {
            throw new RuntimeException('Файл .env не найден или не читается');
        }
        foreach (file(ENV_FILE, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) as $line) {
            $line = trim($line);
            if ($line === '' || $line[0] === '#' || !str_contains($line, '=')) {
                continue;
            }
            [$k, $v] = explode('=', $line, 2);
            $v = trim($v);
            if (strlen($v) >= 2 && ($v[0] === '"' || $v[0] === "'") && $v[-1] === $v[0]) {
                $v = substr($v, 1, -1);
            }
            $vars[trim($k)] = $v;
        }
    }
    if (isset($vars[$key]) && $vars[$key] !== '') {
        return $vars[$key];
    }
    if ($default === null) {
        throw new RuntimeException("В .env не задано значение $key");
    }
    return $default;
}

function db(): PDO
{
    static $pdo = null;
    if ($pdo === null) {
        $dsn = sprintf('mysql:host=%s;port=%s;dbname=%s;charset=utf8mb4',
            env('DB_HOST', '127.0.0.1'), env('DB_PORT', '3306'), env('DB_NAME'));
        $pdo = new PDO($dsn, env('DB_USER'), env('DB_PASS', ''), [
            PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
            PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
            PDO::ATTR_EMULATE_PREPARES => false,
        ]);
        migrate($pdo);
    }
    return $pdo;
}

function migrate(PDO $pdo): void
{
    $pdo->exec('CREATE TABLE IF NOT EXISTS totp_accounts (
        id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        label VARCHAR(190) NOT NULL,
        issuer VARCHAR(190) NOT NULL DEFAULT "",
        secret_enc TEXT NOT NULL,
        digits TINYINT UNSIGNED NOT NULL DEFAULT 6,
        period SMALLINT UNSIGNED NOT NULL DEFAULT 30,
        algo VARCHAR(8) NOT NULL DEFAULT "sha1",
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    ) CHARACTER SET utf8mb4');
    $pdo->exec('CREATE TABLE IF NOT EXISTS login_attempts (
        id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        ip VARCHAR(45) NOT NULL,
        at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        INDEX (ip, at)
    ) CHARACTER SET utf8mb4');
}

// ---------- Шифрование секретов (AES-256-GCM, ключ из APP_KEY) ----------

function app_key(): string
{
    $k = env('APP_KEY');
    if (strlen($k) < 32) {
        throw new RuntimeException('APP_KEY в .env должен быть не короче 32 символов');
    }
    return hash('sha256', $k, true);
}

function encrypt_secret(string $plain): string
{
    $iv = random_bytes(12);
    $ct = openssl_encrypt($plain, 'aes-256-gcm', app_key(), OPENSSL_RAW_DATA, $iv, $tag);
    if ($ct === false) {
        throw new RuntimeException('Не удалось зашифровать секрет');
    }
    return base64_encode($iv . $tag . $ct);
}

function decrypt_secret(string $blob): string
{
    $raw = base64_decode($blob, true);
    if ($raw === false || strlen($raw) < 29) {
        throw new RuntimeException('Повреждённый секрет в БД');
    }
    $plain = openssl_decrypt(substr($raw, 28), 'aes-256-gcm', app_key(), OPENSSL_RAW_DATA,
        substr($raw, 0, 12), substr($raw, 12, 16));
    if ($plain === false) {
        throw new RuntimeException('Не удалось расшифровать секрет (неверный APP_KEY?)');
    }
    return $plain;
}

// ---------- TOTP (RFC 6238) ----------

const B32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function base32_encode(string $bin): string
{
    $bits = '';
    foreach (str_split($bin) as $c) {
        $bits .= str_pad(decbin(ord($c)), 8, '0', STR_PAD_LEFT);
    }
    $out = '';
    foreach (str_split($bits, 5) as $chunk) {
        $out .= B32[bindec(str_pad($chunk, 5, '0'))];
    }
    return $out;
}

/** Возвращает null, если строка — не корректный base32. */
function base32_decode(string $s): ?string
{
    $s = strtoupper(preg_replace('/[\s=-]+/', '', $s));
    if ($s === '' || !preg_match('/^[A-Z2-7]+$/', $s)) {
        return null;
    }
    $bits = '';
    foreach (str_split($s) as $c) {
        $bits .= str_pad(decbin(strpos(B32, $c)), 5, '0', STR_PAD_LEFT);
    }
    $out = '';
    foreach (str_split($bits, 8) as $byte) {
        if (strlen($byte) === 8) {
            $out .= chr(bindec($byte));
        }
    }
    return $out;
}

function totp(string $secretBase32, int $digits = 6, int $period = 30, string $algo = 'sha1', ?int $time = null): string
{
    $key = base32_decode($secretBase32);
    if ($key === null) {
        throw new InvalidArgumentException('Некорректный base32-секрет');
    }
    $counter = intdiv($time ?? time(), $period);
    $hash = hash_hmac($algo, pack('J', $counter), $key, true);
    $o = ord($hash[strlen($hash) - 1]) & 0x0F;
    $bin = ((ord($hash[$o]) & 0x7F) << 24) | (ord($hash[$o + 1]) << 16)
        | (ord($hash[$o + 2]) << 8) | ord($hash[$o + 3]);
    return str_pad((string)($bin % (10 ** $digits)), $digits, '0', STR_PAD_LEFT);
}

function random_secret(int $bytes = 20): string
{
    return base32_encode(random_bytes($bytes));
}

// ---------- Сессия, CSRF, вход ----------

function start_session(): void
{
    session_set_cookie_params([
        'httponly' => true,
        'samesite' => 'Strict',
        'secure' => !empty($_SERVER['HTTPS']),
    ]);
    session_name('twofa');
    session_start();
}

function csrf_token(): string
{
    return $_SESSION['csrf'] ??= bin2hex(random_bytes(32));
}

function csrf_check(): void
{
    $t = $_POST['csrf'] ?? $_SERVER['HTTP_X_CSRF'] ?? '';
    if (!is_string($t) || !hash_equals(csrf_token(), $t)) {
        http_response_code(403);
        exit('Неверный CSRF-токен');
    }
}

function check_admin(string $user, string $pass): bool
{
    $okUser = hash_equals(env('ADMIN_USER'), $user);
    $expected = env('ADMIN_PASSWORD');
    $okPass = str_starts_with($expected, '$')
        ? password_verify($pass, $expected)
        : hash_equals($expected, $pass);
    return $okUser && $okPass;
}

function client_ip(): string
{
    return $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0';
}

function too_many_attempts(): bool
{
    $st = db()->prepare('SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND at > (NOW() - INTERVAL 15 MINUTE)');
    $st->execute([client_ip()]);
    return (int)$st->fetchColumn() >= 5;
}

function record_failed_attempt(): void
{
    db()->prepare('INSERT INTO login_attempts (ip) VALUES (?)')->execute([client_ip()]);
    db()->exec('DELETE FROM login_attempts WHERE at < (NOW() - INTERVAL 1 DAY)');
}

function h(string $s): string
{
    return htmlspecialchars($s, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}
