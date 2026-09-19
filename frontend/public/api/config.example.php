<?php
/**
 * Боевой конфиг PHP-прокси. Скопировать в config.php рядом с index.php
 * и залить на хостинг ОДИН РАЗ (deploy-front его не перезаписывает).
 * config.php в git не попадает.
 */
return [
    // Go API. На проде — только https.
    'upstream' => 'https://api.kupol.vladinc.ru',

    // Общий секрет с Go (KUPOL_PROXY_SECRET), hex или base64, не меньше 32 байт.
    // Сгенерировать: openssl rand -hex 32
    'secret' => 'ЗАМЕНИТЬ_НА_СЕКРЕТ',

    // Если хостинг сам стоит за обратным прокси и REMOTE_ADDR — его адрес,
    // укажите ключ $_SERVER с реальным IP, например 'HTTP_X_REAL_IP'
    // или 'HTTP_X_FORWARDED_FOR' (берётся последний элемент списка).
    // Проверьте по /api/health: client_ip должен быть вашим IP.
    'ip_header' => null,

    'connect_timeout' => 5,   // сек
    'timeout'         => 30,  // сек, весь запрос
];
