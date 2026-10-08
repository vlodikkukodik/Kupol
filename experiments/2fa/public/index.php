<?php
declare(strict_types=1);

// На хостинге src/ лежит рядом с index.php, локально — на уровень выше public/.
require (is_dir(__DIR__ . '/src') ? __DIR__ : dirname(__DIR__)) . '/src/lib.php';

header('X-Frame-Options: DENY');
header('X-Content-Type-Options: nosniff');
header("Content-Security-Policy: default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'");
header('Cache-Control: no-store');

start_session();

try {
    $pdo = db();
} catch (Throwable $e) {
    http_response_code(500);
    exit('Ошибка конфигурации/БД: ' . h($e->getMessage()));
}

$action = $_GET['a'] ?? '';
$error = '';

/** Разбирает otpauth://totp/Issuer:label?secret=...&issuer=...&digits=...&period=...&algorithm=... */
function parse_otpauth(string $uri): ?array
{
    $p = parse_url($uri);
    if (!$p || ($p['scheme'] ?? '') !== 'otpauth' || ($p['host'] ?? '') !== 'totp') {
        return null;
    }
    parse_str($p['query'] ?? '', $q);
    $label = rawurldecode(ltrim($p['path'] ?? '', '/'));
    $issuer = (string)($q['issuer'] ?? '');
    if (str_contains($label, ':')) {
        [$i, $label] = array_map('trim', explode(':', $label, 2));
        $issuer = $issuer ?: $i;
    }
    return [
        'label' => $label, 'issuer' => $issuer, 'secret' => (string)($q['secret'] ?? ''),
        'digits' => (int)($q['digits'] ?? 6), 'period' => (int)($q['period'] ?? 30),
        'algo' => strtolower((string)($q['algorithm'] ?? 'sha1')),
    ];
}

function otpauth_uri(array $a, string $secret): string
{
    $name = ($a['issuer'] !== '' ? rawurlencode($a['issuer']) . ':' : '') . rawurlencode($a['label']);
    $q = ['secret' => $secret, 'digits' => $a['digits'], 'period' => $a['period'],
        'algorithm' => strtoupper($a['algo'])];
    if ($a['issuer'] !== '') {
        $q['issuer'] = $a['issuer'];
    }
    return 'otpauth://totp/' . $name . '?' . http_build_query($q, '', '&', PHP_QUERY_RFC3986);
}

// ---------- Вход / выход ----------

if ($action === 'login' && $_SERVER['REQUEST_METHOD'] === 'POST') {
    csrf_check();
    if (too_many_attempts()) {
        $error = 'Слишком много попыток. Подождите 15 минут.';
    } elseif (check_admin((string)($_POST['user'] ?? ''), (string)($_POST['pass'] ?? ''))) {
        session_regenerate_id(true);
        $_SESSION['admin'] = true;
        header('Location: ./');
        exit;
    } else {
        record_failed_attempt();
        $error = 'Неверный логин или пароль.';
    }
}

if ($action === 'logout' && $_SERVER['REQUEST_METHOD'] === 'POST') {
    csrf_check();
    $_SESSION = [];
    session_destroy();
    header('Location: ./');
    exit;
}

$logged = !empty($_SESSION['admin']);

if (!$logged) {
    if ($action === 'codes') {
        http_response_code(401);
        header('Content-Type: application/json');
        exit('{"error":"unauthorized"}');
    }
    render_login($error);
    exit;
}

// ---------- Действия администратора ----------

if ($action === 'codes') {
    header('Content-Type: application/json');
    $now = time();
    $out = [];
    foreach ($pdo->query('SELECT * FROM totp_accounts ORDER BY id') as $a) {
        try {
            $secret = decrypt_secret($a['secret_enc']);
            $out[] = [
                'id' => (int)$a['id'],
                'code' => totp($secret, (int)$a['digits'], (int)$a['period'], $a['algo'], $now),
                'remaining' => (int)$a['period'] - ($now % (int)$a['period']),
                'period' => (int)$a['period'],
            ];
        } catch (Throwable $e) {
            $out[] = ['id' => (int)$a['id'], 'error' => $e->getMessage()];
        }
    }
    echo json_encode(['accounts' => $out]);
    exit;
}

if ($action === 'add' && $_SERVER['REQUEST_METHOD'] === 'POST') {
    csrf_check();
    $a = null;
    $raw = trim((string)($_POST['secret'] ?? ''));
    if (str_starts_with($raw, 'otpauth://')) {
        $a = parse_otpauth($raw);
        if (!$a) {
            $error = 'Не удалось разобрать otpauth-ссылку.';
        }
    } else {
        $a = [
            'label' => trim((string)($_POST['label'] ?? '')),
            'issuer' => trim((string)($_POST['issuer'] ?? '')),
            'secret' => $raw,
            'digits' => (int)($_POST['digits'] ?? 6),
            'period' => (int)($_POST['period'] ?? 30),
            'algo' => strtolower((string)($_POST['algo'] ?? 'sha1')),
        ];
    }
    if ($a && $error === '') {
        $generated = false;
        if ($a['secret'] === '') {
            $a['secret'] = random_secret();
            $generated = true;
        }
        $norm = base32_decode($a['secret']);
        if ($a['label'] === '') {
            $error = 'Укажите название аккаунта.';
        } elseif ($norm === null || strlen($norm) < 10) {
            $error = 'Секрет должен быть корректным base32 (не короче 16 символов).';
        } elseif (!in_array($a['digits'], [6, 7, 8], true)) {
            $error = 'Число цифр: 6, 7 или 8.';
        } elseif ($a['period'] < 10 || $a['period'] > 120) {
            $error = 'Период: от 10 до 120 секунд.';
        } elseif (!in_array($a['algo'], ['sha1', 'sha256', 'sha512'], true)) {
            $error = 'Алгоритм: SHA1, SHA256 или SHA512.';
        } else {
            $secret = base32_encode($norm);
            $pdo->prepare('INSERT INTO totp_accounts (label, issuer, secret_enc, digits, period, algo) VALUES (?,?,?,?,?,?)')
                ->execute([$a['label'], $a['issuer'], encrypt_secret($secret), $a['digits'], $a['period'], $a['algo']]);
            $_SESSION['flash'] = [
                'title' => $generated ? 'Секрет создан' : 'Аккаунт сохранён',
                'secret' => $secret,
                'uri' => otpauth_uri($a, $secret),
            ];
            header('Location: ./');
            exit;
        }
    }
}

if ($action === 'delete' && $_SERVER['REQUEST_METHOD'] === 'POST') {
    csrf_check();
    $pdo->prepare('DELETE FROM totp_accounts WHERE id = ?')->execute([(int)($_POST['id'] ?? 0)]);
    header('Location: ./');
    exit;
}

$flash = $_SESSION['flash'] ?? null;
unset($_SESSION['flash']);
$accounts = $pdo->query('SELECT id, label, issuer, digits, period, algo FROM totp_accounts ORDER BY id')->fetchAll();
render_app($accounts, $flash, $error);

// ---------- Представления ----------

function page_head(string $title): void
{
    echo '<!doctype html><html lang="ru"><head><meta charset="utf-8">'
        . '<meta name="viewport" content="width=device-width,initial-scale=1">'
        . '<meta name="robots" content="noindex"><title>' . h($title) . '</title><style>'
        . ':root{--bg:#0f1220;--card:#181c30;--fg:#e8eaf6;--mut:#8b91b5;--acc:#6c7bff;--err:#ff6b81;--ok:#4cd9a0}'
        . '*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,sans-serif;padding:16px}'
        . 'main{max-width:720px;margin:0 auto}h1{font-size:1.4rem}h2{font-size:1.1rem;margin:0 0 12px}'
        . '.card{background:var(--card);border-radius:14px;padding:18px;margin:0 0 16px}'
        . 'label{display:block;color:var(--mut);font-size:.85rem;margin:10px 0 4px}'
        . 'input,select{width:100%;padding:10px;border-radius:8px;border:1px solid #2d3357;background:#0f1220;color:var(--fg);font:inherit}'
        . 'button{padding:10px 16px;border:0;border-radius:8px;background:var(--acc);color:#fff;font:inherit;cursor:pointer;margin-top:14px}'
        . 'button.ghost{background:transparent;border:1px solid #2d3357;color:var(--mut);margin:0}'
        . '.row{display:flex;gap:10px}.row>*{flex:1}.err{color:var(--err)}.ok{color:var(--ok)}'
        . '.acc{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}'
        . '.code{font:600 2rem ui-monospace,monospace;letter-spacing:.15em;cursor:pointer}'
        . '.mut{color:var(--mut);font-size:.85rem}.bar{height:4px;background:#2d3357;border-radius:2px;margin-top:10px}'
        . '.bar i{display:block;height:100%;background:var(--acc);border-radius:2px;transition:width 1s linear}'
        . 'code{word-break:break-all;background:#0f1220;padding:2px 6px;border-radius:6px}'
        . '</style></head><body><main>';
}

function render_login(string $error): void
{
    page_head('Вход');
    echo '<h1>2FA — вход</h1><form class="card" method="post" action="?a=login">'
        . '<input type="hidden" name="csrf" value="' . h(csrf_token()) . '">'
        . ($error ? '<p class="err">' . h($error) . '</p>' : '')
        . '<label>Логин</label><input name="user" autocomplete="username" required autofocus>'
        . '<label>Пароль</label><input name="pass" type="password" autocomplete="current-password" required>'
        . '<button>Войти</button></form></main></body></html>';
}

function render_app(array $accounts, ?array $flash, string $error): void
{
    $csrf = h(csrf_token());
    page_head('2FA коды');
    echo '<div class="acc"><h1>2FA коды</h1><form method="post" action="?a=logout">'
        . '<input type="hidden" name="csrf" value="' . $csrf . '"><button class="ghost">Выйти</button></form></div>';

    if ($flash) {
        echo '<div class="card"><h2 class="ok">' . h($flash['title']) . '</h2>'
            . '<p class="mut">Секрет показан один раз. Добавьте его в приложение-аутентификатор, если нужно.</p>'
            . '<p>Секрет: <code>' . h($flash['secret']) . '</code></p>'
            . '<p>otpauth: <code>' . h($flash['uri']) . '</code></p></div>';
    }

    echo '<div class="card"><h2>Добавить / создать</h2>'
        . ($error ? '<p class="err">' . h($error) . '</p>' : '')
        . '<form method="post" action="?a=add"><input type="hidden" name="csrf" value="' . $csrf . '">'
        . '<div class="row"><div><label>Название (напр. email)</label><input name="label"></div>'
        . '<div><label>Сервис (issuer)</label><input name="issuer"></div></div>'
        . '<label>Секрет (base32) или otpauth:// ссылка — пусто = создать новый</label>'
        . '<input name="secret" autocomplete="off" spellcheck="false">'
        . '<div class="row"><div><label>Цифр</label><select name="digits"><option>6</option><option>7</option><option>8</option></select></div>'
        . '<div><label>Период, сек</label><input name="period" type="number" value="30" min="10" max="120"></div>'
        . '<div><label>Алгоритм</label><select name="algo"><option value="sha1">SHA1</option>'
        . '<option value="sha256">SHA256</option><option value="sha512">SHA512</option></select></div></div>'
        . '<button>Сохранить</button></form></div>';

    echo '<div id="list">';
    if (!$accounts) {
        echo '<p class="mut">Пока нет ни одного аккаунта.</p>';
    }
    foreach ($accounts as $a) {
        echo '<div class="card" data-id="' . (int)$a['id'] . '"><div class="acc"><div>'
            . '<strong>' . h($a['label']) . '</strong>'
            . ($a['issuer'] !== '' ? ' <span class="mut">' . h($a['issuer']) . '</span>' : '')
            . '<div class="code" title="Нажмите, чтобы скопировать">······</div></div>'
            . '<form method="post" action="?a=delete" onsubmit="return confirm(\'Удалить аккаунт и секрет?\')">'
            . '<input type="hidden" name="csrf" value="' . $csrf . '"><input type="hidden" name="id" value="' . (int)$a['id'] . '">'
            . '<button class="ghost">Удалить</button></form></div>'
            . '<div class="bar"><i style="width:100%"></i></div></div>';
    }
    echo '</div>';

    echo <<<'JS'
<script>
const state = {};
function fmt(c){ return c.length === 6 ? c.slice(0,3)+' '+c.slice(3) : c.slice(0,4)+' '+c.slice(4); }
async function refresh(){
  try {
    const r = await fetch('?a=codes', {cache:'no-store'});
    if (r.status === 401) { location.reload(); return; }
    const d = await r.json();
    for (const a of d.accounts) state[a.id] = {...a, at: Date.now()};
  } catch (e) {}
  paint();
}
function paint(){
  let need = false;
  document.querySelectorAll('#list .card').forEach(card => {
    const s = state[card.dataset.id]; if (!s) { return; }
    const code = card.querySelector('.code');
    if (s.error) { code.textContent = s.error; code.style.fontSize = '.9rem'; return; }
    const left = s.remaining - (Date.now() - s.at) / 1000;
    if (left <= 0) { need = true; }
    code.textContent = fmt(s.code);
    card.querySelector('.bar i').style.width = Math.max(0, left / s.period * 100) + '%';
  });
  if (need) { refresh(); }
}
document.addEventListener('click', e => {
  if (e.target.classList.contains('code') && navigator.clipboard) {
    navigator.clipboard.writeText(e.target.textContent.replace(/\s/g, ''));
  }
});
refresh(); setInterval(paint, 500);
</script></main></body></html>
JS;
}
