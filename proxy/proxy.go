package proxy

import (
    "io"
    "os"
    "net"
    "net/http"
    "time"
    "sync"
    "embed"
    "errors"
    "context"
    "strings"
    "encoding/json"
    "text/template"

    at "github.com/prometheus/alertmanager/template"
    "github.com/rchnmy/gossip/log"
)

type Proxy struct {
    token     string
    template  *template.Template
    transport *http.Transport
    meta      *store
}

type store struct {
    sync.RWMutex
    data map[string]*alert
}

type alert struct {
    messageID int
    startsAt  time.Time
}

// https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#telo-zaprosa-json
type payload struct {
    DisablePreview bool   `json:"disable_web_page_preview"`
    ReplyID        int    `json:"reply_message_id,omitempty"`
    ChatID         string `json:"chat_id"`
    Text           string `json:"text"`
}

//go:embed static
var efs embed.FS

const (
    addr    = "localhost:9055"
    api_url = "https://botapi.messenger.yandex.net/bot/v1/messages/sendText/"
)

func Deploy() *Proxy {
    s, k := os.LookupEnv("token")
    if !k || s == "" {
        log.Fatal().Err(errors.New("missing token")).Send()
    }

    t, err := template.New("common.tmpl").Funcs(template.FuncMap{
        "upper":  strings.ToUpper,
    }).ParseFS(efs, "static/*.tmpl")
    if err != nil {
        log.Fatal().Err(err).Send()
    }

    return &Proxy {
        token: s,
        template: t,
        transport: &http.Transport {
            DialContext: func(c context.Context, n, a string) (net.Conn, error) {
                conn, err := (&net.Dialer {
                    Timeout:   10 * time.Second,
                    KeepAlive: 10 * time.Second,
                }).DialContext(c, n, a)
                if err != nil {
                    log.Err().Err(err).Msg("dial failed")
                }
                return conn, err
            },
            MaxIdleConns: 16,
            MaxIdleConnsPerHost: 16,
            IdleConnTimeout: 6 * time.Hour,
        },
        meta: &store {
            data: make(map[string]*alert, 0),
        },
    }
}

func Serve(c context.Context, m *http.ServeMux) {
    s := &http.Server {
        Addr:         addr,
        Handler:      m,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
    }
    go func() {
        log.Info().Msgf("server started at %s", addr)
        err := s.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            log.Fatal().Err(err).Msg("shutting down")
            return
        }
    }()

    for {
        <- c.Done()
        log.Info().Msg("shutting down")
        ct, done := context.WithTimeout(context.Background(), 5 * time.Second)
        if err := s.Shutdown(ct); err != nil {
            log.Err().Err(err).Msg("shutdown error")
        }
        done()
        return
    }
}

func Probe(rw http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        rw.WriteHeader(405)
        log.Err().Err(errors.New("not allowed")).Str("method", r.Method).Send()
        return
    }
    rw.WriteHeader(200)
}

func Bounce(p *Proxy) http.HandlerFunc {
    sem := make(chan struct{}, 32)

    return func(rw http.ResponseWriter, r *http.Request) {
        sem <- struct{}{}
        defer func() {
            <- sem
        }()

        switch {
        case r.Method != "POST":
            rw.WriteHeader(405)
            log.Err().Err(errors.New("not allowed")).Str("method", r.Method).Send()
            return
        case len(r.URL.Path) < 2:
            rw.WriteHeader(401)
            log.Err().Err(errors.New("missing chat_id")).Str("path", r.URL.Path).Send()
            return
        case r.Body == nil:
            rw.WriteHeader(400)
            log.Err().Err(errors.New("empty request")).Send()
            return
        }

        d := &at.Data{}
        if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
            rw.WriteHeader(500)
            log.Err().Err(err).Msg("failed to decode request")
            return
        }
        dropBody(r.Body)
        scc := make(chan int)
        go p.send(scc, d, r.URL.Path[1:])
        rw.WriteHeader(<- scc)
    }
}

func(p *Proxy) send(scc chan<- int, d *at.Data, c string) {
    for _, a := range d.Alerts {
        req, err := p.create(a, c)
        if err != nil {
            scc <- 500
            log.Err().Err(err).Str("chat_id", c).Msg("failed to create request")
            break
        }

        res, err := p.transport.RoundTrip(req)
        if err != nil {
            scc <- 502
            log.Err().Err(err).Str("chat_id", c).Msgf("failed to reach %s", api_url)
            break
        }
        scc <- res.StatusCode
        if 4 <= res.StatusCode / 100 {
            errHandle(res.Body, c)
            break
        }
        log.Info().Str("chat_id", c).Msg("notify success")
        if a.Status == "resolved" {
            continue
        }
        // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#primer-uspeshnogo-otveta
        r := struct{ MessageID int `json:"message_id"` }{}
        if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
            log.Err().Err(err).Str("chat_id", c).Msg("failed to decode response")
            continue
        }
        p.put(r.MessageID, a.StartsAt, a.Fingerprint)
        dropBody(res.Body)
    }
    close(scc)
}

func(p *Proxy) create(a at.Alert, c string) (*http.Request, error) {
    s := strings.Builder{}
    if err := p.template.Execute(&s, a); err != nil {
        return nil, err
    }
    l := payload {
        DisablePreview: true,
        ChatID: c,
        Text: s.String(),
    }
    s.Reset()
    if a.Status == "resolved" {
       l.ReplyID = p.pick(a.Fingerprint)
    }

    pr, pw := io.Pipe()
    go func() {
        defer pw.Close()
        if err := json.NewEncoder(pw).Encode(&l); err != nil {
            log.Err().Err(err).Str("chat_id", c).Msg("failed to encode request")
            return
        }
    }()
    r, err := http.NewRequest("POST", api_url, pr)
    if err != nil {
        return nil, err
    }
    // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#zagolovki
    r.Header.Set("Content-Type", "application/json")
    r.Header.Set("Authorization", p.token)
    return r, nil
}

func errHandle(rc io.ReadCloser, c string) {
    defer dropBody(rc)
    // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#primer-otveta-s-oshibkoj
    r := struct{ Description string `json:"description"` }{}
    if err := json.NewDecoder(rc).Decode(&r); err != nil {
        log.Err().Err(err).Str("chat_id", c).Msg("notify failed")
    }
    log.Err().Err(errors.New(r.Description)).Str("chat_id", c).Msg("notify failed")
}

func dropBody(rc io.ReadCloser) {
    rc.Close()
    io.Copy(io.Discard, rc)
}

func(p *Proxy) put(i int, t time.Time, f string) {
    p.meta.Lock()
    defer p.meta.Unlock()
    if i <= 0 || t.IsZero() || f == "" {
        return
    }
    if _, k := p.meta.data[f]; k {
        return
    }
    p.meta.data[f] = &alert {
        messageID: i,
        startsAt:  t,
    }
}

func(p *Proxy) pick(f string) int {
    p.meta.Lock()
    defer p.meta.Unlock()
    if f == "" {
        return 0
    }
    if _, k := p.meta.data[f]; !k {
        return 0
    }
    defer delete(p.meta.data, f)
    return p.meta.data[f].messageID
}

func(p *Proxy) Wipe(d time.Duration) {
    p.meta.Lock()
    defer p.meta.Unlock()
    if len(p.meta.data) == 0 {
        return
    }
    for f, a := range p.meta.data {
        if a.startsAt.Before(time.Now().Add(- d)) {
            delete(p.meta.data, f)
        }
    }
}

