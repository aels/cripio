# Cripio

**criminalip.io bulk exporter/dumper**

CLI tool for bulk-exporting search results from [Criminal IP](https://criminalip.io). Authenticates with email/password, requests a full server-side export, polls until ready, downloads the gzip NDJSON artifact, and saves clean JSONL. Single binary, single thread, zero dependencies beyond Go stdlib.

This README is written in Russian, so, please, use a translator.

---

Однопоточная утилита на Go для bulk-экспорта поисковых результатов из Criminal IP. Логинимся по email/паролю, запрашиваем серверный экспорт всех результатов по нашему запросу, поллим статус задачи, скачиваем gzip-архив с NDJSON, сохраняем в чистый JSONL-файл.

Не нужен API-ключ -- нужен только аккаунт на criminalip.io и запрос. Зависимостей нет, только Go stdlib.

## Установка

```bash
go install github.com/aels/cripio@latest
```

## Использование

```bash
cripio -email user@mail.com -password s3cret -q 'port:5060' -o sip.jsonl
```

- `-email` -- email аккаунта на criminalip.io
- `-password` -- пароль
- `-q` -- поисковый запрос (тот же синтаксис, что в веб-интерфейсе: `port:5060`, `country:US` и т.д.)
- `-o` -- путь к выходному JSONL-файлу (по одной JSON-записи на строку)

Прогресс пишем в stderr, по-этому stdout остается чистым:

```
[*] logging in as user@mail.com
[+] login ok
[*] total: 51150 results for "port:5060"
[*] task: c6e43ff0-4ec2-40be-ad84-a283d9f785b5
[*] export: 42%
[*] export: 85%
[*] downloading...
[+] done: 51150 records -> sip.jsonl
```

Результат -- JSONL, по одному JSON-объекту на строку. Поля: `ip_address`, `port`, `banner`, `as_name`, `country` и проч. Дальше делаем что хотим: `jq`, `grep`, парсим скриптами.

## Пример: выгрузить все IP

```bash
cripio -email me@mail.com -password hunter2 -q 'port:5060' -o dump.jsonl
jq -r '.ip_address' dump.jsonl | sort -u > ips.txt
wc -l ips.txt
# 51150
```

## Как это работает

Утилита повторяет ровно тот же цикл, который делает веб-интерфейс Criminal IP при нажатии кнопки "Download":

1. `POST /api/auth/user/login` -- аутентификация, получаем сессийную cookie `criminalip.sid`
2. `GET /api/proxy/search/banner/stats?query=` -- узнаем общее число результатов
3. `GET /api/proxy/download/asset_search_result_download?limit=&query=&total=` -- запускаем серверный экспорт
4. `GET /api/proxy/download/download_status?query=&req_cnt=&task_id=` -- поллим статус задачи каждые 5 сек
5. `GET https://dl.criminalip.io/asset/<task_id>` -- скачиваем gzip NDJSON, распаковываем, сохраняем как JSONL
