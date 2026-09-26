<?php
// Настройки Roundcube для Vladhost. Файл создаёт deploy/prepare-webmail.sh при каждом запуске (правки вручную будут потеряны).

$config = [];

// База: файл SQLite (одновременных пользователей мало, отдельный сервер БД не нужен). Права на файл у пользователя vhwebmail.
$config['db_dsnw'] = 'sqlite:////var/lib/vhwebmail/roundcube.db?mode=0640';

// Почтовый сервер: тот же, что видят клиенты, с проверкой сертификата.
$config['imap_host'] = 'ssl://@MAIL_HOST@:993';
$config['smtp_host'] = 'ssl://@MAIL_HOST@:465';
$config['smtp_user'] = '%u';
$config['smtp_pass'] = '%p';
$config['imap_conn_options'] = ['ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'cafile' => '/etc/ssl/certs/ca-certificates.crt']];
$config['smtp_conn_options'] = ['ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'cafile' => '/etc/ssl/certs/ca-certificates.crt']];
$config['managesieve_host'] = 'tls://@MAIL_HOST@:4190';
$config['managesieve_conn_options'] = ['ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'cafile' => '/etc/ssl/certs/ca-certificates.crt']];
// Автоответчик задаётся в панели (там же даты и пересылка), поэтому в самом Roundcube он выключен; личные правила фильтрации доступны.
$config['managesieve_vacation'] = 0;

// Вход только полным адресом ящика.
$config['username_domain'] = '';
$config['login_autocomplete'] = 0;
$config['login_rate_limit'] = 5;

// Безопасность
$config['des_key'] = '@DES_KEY@';
$config['enable_installer'] = false;
$config['use_https'] = true;
$config['ip_check'] = true;
$config['session_lifetime'] = 60;
$config['session_samesite'] = 'Lax';
$config['x_frame_options'] = 'sameorigin';
$config['password_charset'] = 'UTF-8';
$config['disabled_actions'] = [];

// Файлы и журналы
$config['log_dir'] = '/var/log/vhwebmail/';
$config['temp_dir'] = '/var/lib/vhwebmail/temp/';
$config['log_driver'] = 'file';
$config['max_message_size'] = '25M';
$config['message_cache_lifetime'] = '7d';
$config['imap_cache'] = null;
$config['messages_cache'] = 'db';

// Внешний вид: язык определяется по браузеру (русский и итальянский есть), основная тема Elastic
$config['product_name'] = 'Vladhost';
$config['language'] = null;
$config['skin'] = 'elastic';
$config['support_url'] = '';
$config['plugins'] = ['archive', 'zipdownload', 'managesieve', 'markasjunk'];
$config['markasjunk_spam_mbox'] = 'Junk';
$config['markasjunk_ham_mbox'] = 'INBOX';
$config['markasjunk_learning_driver'] = null; // обучение rspamd делает dovecot при переносе письма в «Спам» и обратно
$config['junk_mbox'] = 'Junk';
$config['drafts_mbox'] = 'Drafts';
$config['sent_mbox'] = 'Sent';
$config['trash_mbox'] = 'Trash';
$config['create_default_folders'] = true;
$config['protect_default_folders'] = true;
