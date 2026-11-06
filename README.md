# Домашнее задание 13 — библиотека и PostgreSQL

## Краткий отчёт

Библиотека на PostgreSQL: книги, читатели, выдачи и возврат.

## Выполнение по заданию

### 1. База данных

Скрипт `sql/init.sql` создаёт связанные таблицы `books`, `readers`, `loans` и уникальный индекс активной выдачи.

### 2. Маршруты

CRUD `/books`, GET и POST `/readers`, POST `/loans`, PUT `/loans/{id}/return`, GET `/loans/active` и `/statistics`.

### 3. Проверка

Проверены добавление книги и читателей, выдача, отказ при повторной выдаче, JOIN активных выдач, возврат и повторная выдача другому читателю.

## Запуск

Локальный PostgreSQL установлен в `%LOCALAPPDATA%\Programs\postgres-labs`, порт 55432. База `library_db` должна быть создана; выполните скрипт sql/init.sql. Откройте PowerShell в этой папке:

```powershell
$env:DATABASE_URL = 'postgres://postgres:YOUR_POSTGRES_PASSWORD@127.0.0.1:55432/library_db?sslmode=disable'
go run .
```

Замените `YOUR_POSTGRES_PASSWORD` паролем своей базы (специальные символы в URL нужно закодировать). Откройте нужный адрес на `http://localhost:8080`. Если PostgreSQL остановлен, запустите его:

```powershell
& "$env:LOCALAPPDATA\Programs\postgres-labs\pgsql\bin\pg_ctl.exe" -D "$env:LOCALAPPDATA\Programs\postgres-labs\data" -l "$env:LOCALAPPDATA\Programs\postgres-labs\server.log" -o "-h 127.0.0.1 -p 55432" -w start
```

На другом компьютере создайте базу `library_db`, выполните `sql/init.sql` через `psql` и задайте свою строку подключения в `DATABASE_URL`.

## Пример добавления

Пока сервер работает, откройте второе окно PowerShell:

```powershell
$body = @{ title="Книга Go"; author="Автор"; year=2024 } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/books -ContentType "application/json; charset=utf-8" -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
```

Чтобы зарегистрировать читателя и выдать ему книгу, используйте ID из ответов:

```powershell
$reader = @{ full_name="Иван Петров"; email="ivan@example.test" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/readers -ContentType "application/json; charset=utf-8" -Body ([System.Text.Encoding]::UTF8.GetBytes($reader))
$loan = @{ book_id=1; reader_id=1 } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/loans -ContentType "application/json" -Body $loan
Invoke-RestMethod http://localhost:8080/loans/active
```

Если ID в ответах отличаются от 1, подставьте их в `$loan`.

## Скриншот выполнения

![Результат работы приложения](docs/result.png)

## Вывод

Выдача и возврат выполняются транзакциями, а блокировка строки защищает книгу от одновременной выдачи.
