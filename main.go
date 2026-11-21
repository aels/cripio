package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	base = "https://search.criminalip.io"
	ua   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type cip struct {
	c *http.Client
}

type apiResp struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newCIP() *cip {
	jar, _ := cookiejar.New(nil)
	return &cip{c: &http.Client{Jar: jar, Timeout: 2 * time.Minute}}
}

func (c *cip) do(method, path string, body io.Reader, ct, ref string) (*apiResp, error) {
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+ref)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-client-language", "en")
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	r, err := c.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	var out apiResp
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *cip) login(email, pw string) error {
	form := url.Values{
		"account_type": {"not_social"},
		"email":        {email},
		"pw":           {pw},
	}
	r, err := c.do("POST", "/api/auth/user/login",
		strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded", "/login")
	if err != nil {
		return err
	}
	if r.Status != 200 {
		return fmt.Errorf("%s", r.Message)
	}
	return nil
}

func (c *cip) count(q string) (int, error) {
	r, err := c.do("GET",
		"/api/proxy/search/banner/stats?"+url.Values{"query": {q}}.Encode(),
		nil, "", "/asset/search")
	if err != nil {
		return 0, err
	}
	var d struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return 0, err
	}
	return d.Count, nil
}

func (c *cip) export(q string, total int) (string, error) {
	p := url.Values{
		"limit": {strconv.Itoa(total)},
		"query": {q},
		"total": {strconv.Itoa(total)},
	}
	r, err := c.do("GET",
		"/api/proxy/download/asset_search_result_download?"+p.Encode(),
		nil, "", "/asset/download")
	if err != nil {
		return "", err
	}
	if r.Status != 200 {
		return "", fmt.Errorf("export: %s", r.Message)
	}
	var d struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return "", err
	}
	if d.TaskID == "" {
		return "", fmt.Errorf("empty task_id")
	}
	return d.TaskID, nil
}

func (c *cip) poll(q string, total int, taskID string) (string, error) {
	p := url.Values{
		"query":   {q},
		"req_cnt": {strconv.Itoa(total)},
		"task_id": {taskID},
	}
	path := "/api/proxy/download/download_status?" + p.Encode()

	for i := 0; i < 120; i++ {
		r, err := c.do("GET", path, nil, "", "/asset/download")
		if err != nil {
			return "", err
		}
		var d struct {
			Complete bool   `json:"complete"`
			Link     string `json:"download_link"`
			Progress struct {
				Pct int `json:"percent"`
			} `json:"progress"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil {
			return "", err
		}
		if d.Complete && d.Link != "" {
			return d.Link, nil
		}
		fmt.Fprintf(os.Stderr, "[*] export: %d%%\n", d.Progress.Pct)
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("export timed out (10 min)")
}

func fetch(link string, dst io.Writer) (int, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", ua)

	dl := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
		Timeout:   10 * time.Minute,
	}
	r, err := dl.Do(req)
	if err != nil {
		return 0, err
	}
	defer r.Body.Close()

	br := bufio.NewReaderSize(r.Body, 256<<10)
	magic, err := br.Peek(2)
	if err != nil {
		return 0, fmt.Errorf("peek: %w", err)
	}

	var rd io.Reader = br
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return 0, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		rd = gz
	}

	w := bufio.NewWriterSize(dst, 256<<10)
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)

	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		w.WriteString(line)
		w.WriteByte('\n')
		n++
	}
	if err := sc.Err(); err != nil {
		return n, err
	}
	return n, w.Flush()
}

func main() {
	email := flag.String("email", "", "account email")
	pw := flag.String("password", "", "account password")
	q := flag.String("q", "", "search query (e.g. port:5060)")
	o := flag.String("o", "", "output JSONL file (default: stdout)")
	flag.Parse()

	if *email == "" || *pw == "" || *q == "" {
		fmt.Fprintln(os.Stderr, "usage: cripio -email EMAIL -password PW -q QUERY [-o FILE.jsonl]")
		os.Exit(1)
	}

	c := newCIP()

	fmt.Fprintf(os.Stderr, "[*] logging in as %s\n", *email)
	if err := c.login(*email, *pw); err != nil {
		fmt.Fprintf(os.Stderr, "[-] login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "[+] login ok")

	total, err := c.count(*q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] count failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[*] total: %d results for %q\n", total, *q)
	if total == 0 {
		fmt.Fprintln(os.Stderr, "[-] nothing found")
		os.Exit(0)
	}

	tid, err := c.export(*q, total)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] export failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[*] task: %s\n", tid)

	link, err := c.poll(*q, total, tid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] poll failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[*] downloading...\n")

	var dst io.WriteCloser = os.Stdout
	if *o != "" {
		f, err := os.Create(*o)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] create %s: %v\n", *o, err)
			os.Exit(1)
		}
		defer f.Close()
		dst = f
	}

	n, err := fetch(link, dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] download failed: %v\n", err)
		os.Exit(1)
	}
	if *o != "" {
		fmt.Fprintf(os.Stderr, "[+] done: %d records -> %s\n", n, *o)
	} else {
		fmt.Fprintf(os.Stderr, "[+] done: %d records\n", n)
	}
}
