package main

import (
    "log"
    "flag"
    "time"
    "path"
    "io"
    "os"
    "os/signal"
    "syscall"
    "context"
    "net"
    "net/url"
    "net/http"
    "net/http/httputil"
    "strings"
    "sync"
    "encoding/json"
    "html/template"

    "gopkg.in/yaml.v3"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
    "golang.org/x/text/cases"
    "golang.org/x/text/language"
    a "github.com/prometheus/alertmanager/template"
)

var conf = struct {
    Hide     bool   `yaml:"disable_preview,flow,omitempty"`
    Addr     string `yaml:"addr,flow"`
    API      string `yaml:"api,flow"`
    Auth     string `yaml:"auth,flow"`
    Receiver map[string]struct {
        Quiet    bool   `yaml:"disable_notification,flow,omitempty"`
        Chat     string `yaml:"chat_id,flow"`
        Template string `yaml:"template,flow"`
    }
}{}

var lo *zap.Logger

func main() {
    shut := make(chan os.Signal, 1)
    signal.Notify(shut, syscall.SIGINT, syscall.SIGTERM)
    ctx, stop := context.WithTimeout(context.Background(), 500 * time.Millisecond)
    defer stop()

    lo = logger()
    defer lo.Sync()

    config()

    mux := http.NewServeMux()
    mux.HandleFunc("/", limiter)
    mux.HandleFunc("/health", prober)

    srv := server(conf.Addr, mux)
    go func() {
        err := srv.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            lo.Fatal("failed to start server",
                zap.String("status", "fail"),
                zap.Error(err),
            )
        }
    }()
    <- shut

    select {
        case <- ctx.Done():
            err := srv.Shutdown(ctx)
            if err != nil {
                lo.Error("shutdown error",
                    zap.String("status", "fail"),
                    zap.Error(err),
                )
            }
    }
    lo.Info("later gator! xoxo",
        zap.String("status", "ok"),
    )
}

func logger() *zap.Logger {
    l := zap.NewProductionEncoderConfig()
    l.LevelKey    = "severity"
    l.MessageKey  = "message"
    l.TimeKey     = "@timestamp"
    l.EncodeTime  = zapcore.RFC3339TimeEncoder
    l.EncodeLevel = zapcore.CapitalLevelEncoder
    lc := &zap.Config {
        EncoderConfig:     l,
        Level:             zap.NewAtomicLevelAt(zap.InfoLevel),
        Development:       false,
        DisableCaller:     false,
        DisableStacktrace: true,
        Sampling:          nil,
        Encoding:          "json",
        OutputPaths: []string {
            "stderr",
            // "/var/log/gossip.log",
        },
        ErrorOutputPaths: []string {
            "stderr",
        },
        InitialFields: map[string]interface{} {
            "version":  "1.0",
            "facility": "gossip",
        },
    }

    lo, err := lc.Build()
    if err != nil {
        log.Fatalf("failed to initialise logger: %+v", err)
    }
    return lo
}

func config() {
    f := flag.String("c", "/etc/gossip/gossip.yml", "config file path")
    flag.Parse()

    fi, err := os.ReadFile(*f)
    if err != nil {
        lo.Fatal("loading configuration failed",
            zap.String("status", "fail"),
            zap.Error(err),
        )
    }

    err = yaml.Unmarshal([]byte(fi), &conf)
    if err != nil {
        lo.Fatal("decoding configuration failed",
            zap.String("status", "fail"),
            zap.String("config", *f),
            zap.Error(err),
        )
    }
}

func server(addr string, mux *http.ServeMux) *http.Server {
    lo.Info("eavesdropping! xoxo",
        zap.String("status", "ok"),
        zap.String("addr", addr),
    )
    return &http.Server {
        Addr:         addr,
        Handler:      mux,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
    }
}

func prober(rw http.ResponseWriter, req *http.Request) {
    switch {
        case req.Method != http.MethodGet:
            rw.WriteHeader(http.StatusMethodNotAllowed)
            lo.Error("not allowed",
                zap.String("status", "fail"),
                zap.String("method", req.Method),
                zap.String("addr", req.RemoteAddr),
            )
        default:
            rw.WriteHeader(http.StatusOK)
    }
}

func limiter(rw http.ResponseWriter, req *http.Request) {
    sema := make(chan struct{}, 32)
    sema <- struct{}{}
    defer func() {
        <- sema
    }()

    switch {
        case req.URL.Path != "/":
            rw.WriteHeader(http.StatusNotFound)
            lo.Error("not found",
                zap.String("status", "fail"),
                zap.String("uri", req.RequestURI),
                zap.String("addr", req.RemoteAddr),
            )
            return
        case req.Method != http.MethodPost:
            rw.WriteHeader(http.StatusMethodNotAllowed)
            lo.Error("not allowed",
                zap.String("status", "fail"),
                zap.String("method", req.Method),
                zap.String("addr", req.RemoteAddr),
            )
            return
        case req.Body == nil:
            rw.WriteHeader(http.StatusBadRequest)
            lo.Error("empty request",
                zap.String("status", "fail"),
            )
            return
    }

    msg := &a.Data{}
    err := json.NewDecoder(req.Body).Decode(&msg)
    if err != nil {
        rw.WriteHeader(http.StatusInternalServerError)
        lo.Error("decoding request body failed",
            zap.String("status", "fail"),
            zap.Error(err),
        )
        return
    }
    defer io.Copy(io.Discard, req.Body)
    defer req.Body.Close()

    name := msg.Receiver
    if name == "" {
        rw.WriteHeader(http.StatusUnauthorized)
        lo.Error("empty receiver",
            zap.String("status", "fail"),
            zap.Error(err),
        )
        return
    } else if _, ok := conf.Receiver[name]; !ok {
        rw.WriteHeader(http.StatusUnauthorized)
        lo.Error("unknown receiver",
            zap.String("status", "fail"),
            zap.String("receiver", name),
            zap.Error(err),
        )
        return
    }

    api, err := url.Parse(conf.API)
    if err != nil {
        lo.Fatal("parsing api url failed",
            zap.String("status", "fail"),
            zap.Error(err),
        )
    }

    prx, err := proxy(name, api, msg)
    if err != nil {
        lo.Fatal("failed to initialise proxy",
            zap.String("status", "fail"),
            zap.Error(err),
        )
    }
    prx.ServeHTTP(rw, req)
}

func proxy(name string, api *url.URL, msg *a.Data) (*httputil.ReverseProxy, error) {
    // https://github.com/golang/go/issues/53002
    return &httputil.ReverseProxy {
        Rewrite: func(req *httputil.ProxyRequest) {
            req.SetURL(api)
            reqModifier(name, msg, req.Out)
        },
        Transport: &http.Transport {
            DialContext: func(ctx context.Context, netw, addr string) (net.Conn, error) {
                conn, err := (&net.Dialer {
                    Timeout: 15 * time.Second,
                }).DialContext(ctx, netw, addr)
                if err != nil {
                    lo.Error("api unavailable",
                        zap.String("status", "fail"),
                        zap.Error(err),
                    )
                }
                return conn, err
            },
            MaxIdleConnsPerHost: 0,
            TLSHandshakeTimeout: 5 * time.Second,
            DisableCompression:  true,
            DisableKeepAlives:   true,
        },
        ModifyResponse: resRegister,
    }, nil
}

func reqModifier(name string, msg *a.Data, req *http.Request) {
    tmpl := conf.Receiver[name].Template
    t, err := template.New(path.Base(tmpl)).Funcs(template.FuncMap {
        "Upper":  cases.Upper(language.Und).String,
        "Title":  cases.Title(language.Und).String,
    }).ParseFiles(tmpl)
    if err != nil {
        lo.Error("failed to parse template",
            zap.String("status", "fail"),
            zap.String("template", tmpl),
            zap.Error(err),
        )
        return
    }

    str := strings.Builder{}
    err = t.Execute(&str, &msg)
    if err != nil {
        lo.Error("rendering template failed",
            zap.String("status", "fail"),
            zap.String("template", tmpl),
            zap.Error(err),
        )
        return
    }

    // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#telo-zaprosa-json
    pl := struct {
        Hide  bool   `json:"disable_web_page_preview,omitempty"`
        Quiet bool   `json:"disable_notification,omitempty"`
        Chat  string `json:"chat_id"`
        Text  string `json:"text"` 
    }{
        Hide:  conf.Hide,
        Quiet: conf.Receiver[name].Quiet,
        Chat:  conf.Receiver[name].Chat,
        Text:  str.String(),
    }
    str.Reset()

    pr, pw := io.Pipe()
    wg := sync.WaitGroup{}

    wg.Add(1)
    go func() {
        defer wg.Done()
        defer pw.Close()

        err = json.NewEncoder(pw).Encode(&pl)
        if err != nil {
            lo.Error("failed to encode payload",
                zap.String("status", "fail"),
                zap.String("receiver", name),
                zap.Error(err),
            )
            return
        }
    }()

    req.Body = pr
    // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#zagolovki
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", conf.Auth)
    // https://github.com/golang/go/blob/master/src/net/http/request.go#L206-L211
    // https://datatracker.ietf.org/doc/html/rfc7230#section-4
    req.TransferEncoding = []string{"chunked"}
}

func resRegister(res *http.Response) error {
    str := strings.Builder{}
    defer str.Reset()
    _, err := io.Copy(&str, res.Body)
    if err != nil {
        lo.Error("failed to copy response body",
            zap.String("status", "fail"),
            zap.Error(err),
        )
        return err
    }

    // https://yandex.ru/dev/messenger/doc/ru/api-requests/message-send-text#primer-uspeshnogo-otveta
    rs := struct {
        Status bool `json:"ok,omitempty"`
    }{}
    err = json.NewDecoder(strings.NewReader(str.String())).Decode(&rs)
    if err != nil {
        lo.Error("failed to decode response body",
            zap.String("status", "fail"),
            zap.Error(err),
        )
        return err
    }

    res.Body = io.NopCloser(strings.NewReader(str.String()))
    if ok := rs.Status; !ok {
        lo.Error("bad response",
            zap.String("status", "fail"),
            zap.String("http", res.Status),
            zap.String("body", str.String()),
        )
        return err
    }
    return nil
}

