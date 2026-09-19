# КУПОЛ

Комитет Управления Паранормальными Объектами и Локациями — текстовый архив в духе КОНТУРа (СССР, аномалии).
Вымышленный проект: всё описанное — художественный вымысел.

- Канон вселенной: [`docs/organization.md`](docs/organization.md)
- Спецификация сайта: [`docs/site.md`](docs/site.md)
- Разработка: [`docs/dev.md`](docs/dev.md)
- Деплой: [`docs/deploy.md`](docs/deploy.md)

## Быстрый старт

```bash
make env     # .env с новым секретом прокси
make dev     # PostgreSQL + Go API + PHP-прокси + Vite -> http://127.0.0.1:5173
make test    # быстрые тесты (Go, PHP-прокси, фронтенд)
```
