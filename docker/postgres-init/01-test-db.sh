#!/bin/sh
# Отдельная БД для интеграционных тестов бэкенда.
set -e
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE DATABASE kupol_test
  ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0;
SQL
