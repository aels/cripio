# Cripio

**criminalip.io bulk exporter/dumper**

CLI tool for bulk-exporting search results from [Criminal IP](https://criminalip.io). Authenticates with email/password, requests a full server-side export, polls until ready, downloads the gzip NDJSON artifact, and saves clean JSONL. Single binary, single thread, zero dependencies beyond Go stdlib.

No API key needed -- just an account on criminalip.io and a query.

## Install

```bash
go install github.com/aels/cripio@latest
```

## Usage

```bash
cripio -email user@mail.com -password s3cret -q 'port:5060' -o sip.jsonl
```

- `-email` -- criminalip.io account email
- `-password` -- account password
- `-q` -- search query (same syntax as the web UI: `port:5060`, `country:US`, etc.)
- `-o` -- output JSONL file path (optional; prints to stdout if omitted)

Progress goes to stderr, so stdout stays clean and pipeable:

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

Output is JSONL -- one JSON object per line. Fields include `ip_address`, `port`, `banner`, `as_name`, `country`, etc. Pipe it into `jq`, `grep`, or parse with scripts.

## Examples

Save to file:

```bash
cripio -email me@mail.com -password hunter2 -q 'port:5060' -o dump.jsonl
```

Pipe straight into `jq`:

```bash
cripio -email me@mail.com -password hunter2 -q 'port:5060' | jq -r '.ip_address' | sort -u > ips.txt
wc -l ips.txt
# 51150
```

## How it works

The tool replays the exact same cycle the Criminal IP web UI does when you hit "Download":

1. `POST /api/auth/user/login` -- authenticate, grab the `criminalip.sid` session cookie
2. `GET /api/proxy/search/banner/stats?query=` -- get the total result count
3. `GET /api/proxy/download/asset_search_result_download?limit=&query=&total=` -- kick off the server-side export
4. `GET /api/proxy/download/download_status?query=&req_cnt=&task_id=` -- poll task status every 5 sec
5. `GET https://dl.criminalip.io/asset/<task_id>` -- download gzip NDJSON, decompress, save as JSONL
